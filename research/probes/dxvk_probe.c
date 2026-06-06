/* dxvk_probe.c — DXVK workload simulator with 3 DXVK-pattern features:
 *   1. Multi-pipeline switching (emulates shader changes)
 *   2. Descriptor set updates (UBO binding per N draws)
 *   3. vkCmd-level call counters (tally each Vulkan API call)
 *
 * Pipeline A: orange quads (rotation via push constants)
 * Pipeline B: blue quads (different FS, different pipeline obj)
 * Switches every 20 draws — exercises DXVK's commitGraphicsState path.
 */

#include <dlfcn.h>
#include <vulkan/vulkan.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <math.h>

#include "dxvk_spirv.h"

#define WIDTH  512
#define HEIGHT 512
#define NUM_FRAMES 20
#define DRAWS_PER_FRAME 100
#define PIPELINE_SWITCH_EVERY 20
#define UBO_BIND_EVERY 5

static double now_ms(void) {
    struct timespec ts; clock_gettime(CLOCK_MONOTONIC, &ts);
    return ts.tv_sec * 1000.0 + ts.tv_nsec / 1000000.0;
}
#define LOG(fmt,...) do{ fprintf(stderr, fmt "\n", ##__VA_ARGS__); fflush(stderr); }while(0)

/* vkCmd-level call counters */
enum { C_begin_rp, C_end_rp, C_bind_pipe, C_bind_vb, C_bind_ib, C_draw,
       C_push, C_set_vp, C_set_sc, C_begin_cb, C_end_cb, C_submit,
       C_alloc_cb, C_reset_cb, C_reset_fence, C_wait_fence,
       C_alloc_mem, C_free_mem, C_create_buf, C_destroy_buf,
       C_map, C_unmap, C_create_img, C_destroy_img, C_bind_img_mem,
       C_get_img_mem_req, C_create_iv, C_destroy_iv,
       C_create_rp, C_destroy_rp, C_create_fb, C_destroy_fb,
       C_create_pipe, C_destroy_pipe, C_create_pl, C_destroy_pl,
       C_create_shader, C_destroy_shader,
       C_create_dsl, C_create_dpool, C_alloc_ds, C_update_ds, C_update_ubo,
       C_COUNT };
static uint32_t counters[C_COUNT];
const char *counter_names[] = {
    "beginRenderPass","endRenderPass","bindPipeline","bindVB","bindIB","drawIndexed",
    "pushConstants","setViewport","setScissor","beginCmdBuf","endCmdBuf","queueSubmit",
    "allocCmdBuf","resetCmdBuf","resetFence","waitFence",
    "allocMem","freeMem","createBuf","destroyBuf",
    "map","unmap","createImg","destroyImg","bindImgMem",
    "getImgMemReq","createImgView","destroyImgView",
    "createRenderPass","destroyRP","createFB","destroyFB",
    "createPipe","destroyPipe","createPLayout","destroyPLayout",
    "createShader","destroyShader",
    "createDSLayout","createDPool","allocDS","updateDS","updateUBO",
};
#define INC(c) counters[c]++

int main(void) {
    setenv("TU_DEBUG", "trace", 1);

    void *icd = dlopen("/data/local/tmp/libvulkan_freedreno.so", RTLD_NOW);
    if (!icd) { LOG("dlopen fail"); return 1; }

    PFN_vkVoidFunction (*gp)(VkInstance,const char*) =
        (PFN_vkVoidFunction(*)(VkInstance,const char*))
        dlsym(icd, "vk_icdGetInstanceProcAddr");
    if (!gp) { LOG("no gp"); return 1; }

    PFN_vkCreateInstance pCreateInstance = (PFN_vkCreateInstance)gp(NULL,"vkCreateInstance");
    VkApplicationInfo ai={VK_STRUCTURE_TYPE_APPLICATION_INFO,0,"dxvk",1,0,VK_API_VERSION_1_2};
    VkInstanceCreateInfo ici={VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO,0,0,&ai,0,0,0,0};
    VkInstance inst=0; pCreateInstance(&ici,0,&inst);
    if(!inst){LOG("no inst");return 1;}

    #define G(fn,name) PFN_##fn p##fn=(PFN_##fn)gp(inst,name)
    PFN_vkDestroyInstance pDI=(PFN_vkDestroyInstance)gp(inst,"vkDestroyInstance");
    PFN_vkEnumeratePhysicalDevices pED=(PFN_vkEnumeratePhysicalDevices)gp(inst,"vkEnumeratePhysicalDevices");
    PFN_vkGetPhysicalDeviceProperties pGP=(PFN_vkGetPhysicalDeviceProperties)gp(inst,"vkGetPhysicalDeviceProperties");
    PFN_vkGetPhysicalDeviceQueueFamilyProperties pGQF=(PFN_vkGetPhysicalDeviceQueueFamilyProperties)gp(inst,"vkGetPhysicalDeviceQueueFamilyProperties");
    PFN_vkCreateDevice pCD=(PFN_vkCreateDevice)gp(inst,"vkCreateDevice");
    PFN_vkDestroyDevice pDD=(PFN_vkDestroyDevice)gp(inst,"vkDestroyDevice");
    PFN_vkGetDeviceQueue pGQ=(PFN_vkGetDeviceQueue)gp(inst,"vkGetDeviceQueue");

    uint32_t nd=0; pED(inst,&nd,0);
    VkPhysicalDevice pd; pED(inst,&nd,&pd);
    VkPhysicalDeviceProperties pr; pGP(pd,&pr);
    LOG("GPU: %s", pr.deviceName);

    uint32_t qc=0; pGQF(pd,&qc,0);
    VkQueueFamilyProperties qp[4]; pGQF(pd,&qc,qp);
    uint32_t qf=UINT32_MAX;
    for(uint32_t i=0;i<qc&&i<4;i++) if(qp[i].queueFlags&VK_QUEUE_GRAPHICS_BIT){qf=i;break;}
    if(qf==UINT32_MAX)return 1;

    float prio=1.0f;
    VkDeviceQueueCreateInfo qci={VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO,0,0,qf,1,&prio};
    VkDeviceCreateInfo dci={VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO,0,0,1,&qci,0,0,0,0,0};
    VkDevice dev=0; pCD(pd,&dci,0,&dev); INC(C_submit); /* device creation counted as submit */
    VkQueue q; pGQ(dev,qf,0,&q);

    G(vkCreateShaderModule,"vkCreateShaderModule"); G(vkDestroyShaderModule,"vkDestroyShaderModule");
    G(vkCreatePipelineLayout,"vkCreatePipelineLayout"); G(vkDestroyPipelineLayout,"vkDestroyPipelineLayout");
    G(vkCreateGraphicsPipelines,"vkCreateGraphicsPipelines"); G(vkDestroyPipeline,"vkDestroyPipeline");
    G(vkCreateRenderPass,"vkCreateRenderPass"); G(vkDestroyRenderPass,"vkDestroyRenderPass");
    G(vkCmdBeginRenderPass,"vkCmdBeginRenderPass"); G(vkCmdEndRenderPass,"vkCmdEndRenderPass");
    G(vkCmdBindPipeline,"vkCmdBindPipeline"); G(vkCmdBindVertexBuffers,"vkCmdBindVertexBuffers");
    G(vkCmdBindIndexBuffer,"vkCmdBindIndexBuffer"); G(vkCmdDrawIndexed,"vkCmdDrawIndexed");
    G(vkCmdPushConstants,"vkCmdPushConstants"); G(vkCmdSetViewport,"vkCmdSetViewport");
    G(vkCmdSetScissor,"vkCmdSetScissor");
    G(vkCreateImage,"vkCreateImage"); G(vkDestroyImage,"vkDestroyImage");
    G(vkGetImageMemoryRequirements,"vkGetImageMemoryRequirements");
    G(vkAllocateMemory,"vkAllocateMemory"); G(vkFreeMemory,"vkFreeMemory");
    G(vkBindImageMemory,"vkBindImageMemory");
    G(vkCreateImageView,"vkCreateImageView"); G(vkDestroyImageView,"vkDestroyImageView");
    G(vkCreateFramebuffer,"vkCreateFramebuffer"); G(vkDestroyFramebuffer,"vkDestroyFramebuffer");
    G(vkCreateCommandPool,"vkCreateCommandPool"); G(vkDestroyCommandPool,"vkDestroyCommandPool");
    G(vkAllocateCommandBuffers,"vkAllocateCommandBuffers");
    G(vkBeginCommandBuffer,"vkBeginCommandBuffer"); G(vkEndCommandBuffer,"vkEndCommandBuffer");
    G(vkQueueSubmit,"vkQueueSubmit"); G(vkResetCommandBuffer,"vkResetCommandBuffer");
    G(vkCreateFence,"vkCreateFence"); G(vkDestroyFence,"vkDestroyFence");
    G(vkWaitForFences,"vkWaitForFences"); G(vkResetFences,"vkResetFences");
    G(vkCreateBuffer,"vkCreateBuffer"); G(vkDestroyBuffer,"vkDestroyBuffer");
    G(vkGetBufferMemoryRequirements,"vkGetBufferMemoryRequirements");
    G(vkBindBufferMemory,"vkBindBufferMemory");
    G(vkMapMemory,"vkMapMemory"); G(vkUnmapMemory,"vkUnmapMemory");
    G(vkCreateDescriptorSetLayout,"vkCreateDescriptorSetLayout");
    G(vkDestroyDescriptorSetLayout,"vkDestroyDescriptorSetLayout");
    G(vkCreateDescriptorPool,"vkCreateDescriptorPool");
    G(vkDestroyDescriptorPool,"vkDestroyDescriptorPool");
    G(vkAllocateDescriptorSets,"vkAllocateDescriptorSets");
    G(vkUpdateDescriptorSets,"vkUpdateDescriptorSets");
    #undef G

    INC(C_submit); /* undo the INC at device creation, it was wrong */
    counters[C_submit] = 0;

    /* ---- Shader modules (2 FS variants) ---- */
    VkShaderModuleCreateInfo smci={VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO,0,0,dxvk_vs_spv_size,dxvk_vs_spv};
    VkShaderModule vs; pvkCreateShaderModule(dev,&smci,0,&vs); INC(C_create_shader);
    smci.pCode=dxvk_fs_a_spv; smci.codeSize=dxvk_fs_a_spv_size;
    VkShaderModule fs_a; pvkCreateShaderModule(dev,&smci,0,&fs_a); INC(C_create_shader);
    smci.pCode=dxvk_fs_b_spv; smci.codeSize=dxvk_fs_b_spv_size;
    VkShaderModule fs_b; pvkCreateShaderModule(dev,&smci,0,&fs_b); INC(C_create_shader);

    /* ---- Descriptor set layout + pool (UBO at set=0, binding=0) ---- */
    VkDescriptorSetLayoutBinding dsb={0,VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER,1,VK_SHADER_STAGE_VERTEX_BIT,0};
    VkDescriptorSetLayoutCreateInfo dsli={VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO,0,0,1,&dsb};
    VkDescriptorSetLayout dsl; pvkCreateDescriptorSetLayout(dev,&dsli,0,&dsl); INC(C_create_dsl);

    VkPushConstantRange pcr={VK_SHADER_STAGE_VERTEX_BIT,0,32};
    VkPipelineLayoutCreateInfo plci={VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO,0,0,1,&dsl,1,&pcr};
    VkPipelineLayout pl; pvkCreatePipelineLayout(dev,&plci,0,&pl); INC(C_create_pl);

    /* ---- UBO buffer for descriptor updates ---- */
    float ubo_data[16]; /* 64 bytes per-frame */
    VkBuffer ubo; VkDeviceMemory ubo_mem;
    { VkBufferCreateInfo bci={VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO,0,0,sizeof(ubo_data),VK_BUFFER_USAGE_UNIFORM_BUFFER_BIT,VK_SHARING_MODE_EXCLUSIVE,0,0};
      pvkCreateBuffer(dev,&bci,0,&ubo); INC(C_create_buf);
      VkMemoryRequirements mr; pvkGetBufferMemoryRequirements(dev,ubo,&mr); INC(C_get_img_mem_req);
      VkMemoryAllocateInfo mai={VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,0,mr.size,0};
      pvkAllocateMemory(dev,&mai,0,&ubo_mem); INC(C_alloc_mem);
      pvkBindBufferMemory(dev,ubo,ubo_mem,0); }

    /* Descriptor pool + set */
    VkDescriptorPoolSize dps={VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER,1};
    VkDescriptorPoolCreateInfo dpci={VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO,0,0,1,1,&dps};
    VkDescriptorPool dpool; pvkCreateDescriptorPool(dev,&dpci,0,&dpool); INC(C_create_dpool);
    VkDescriptorSetAllocateInfo dsai={VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO,0,dpool,1,&dsl};
    VkDescriptorSet ds; pvkAllocateDescriptorSets(dev,&dsai,&ds); INC(C_alloc_ds);

    /* ---- Two pipelines ---- */
    VkVertexInputBindingDescription vib={0,8,VK_VERTEX_INPUT_RATE_VERTEX};
    VkVertexInputAttributeDescription via={0,0,VK_FORMAT_R32G32_SFLOAT,0};
    VkPipelineVertexInputStateCreateInfo vici={VK_STRUCTURE_TYPE_PIPELINE_VERTEX_INPUT_STATE_CREATE_INFO,0,0,1,&vib,1,&via};
    VkPipelineInputAssemblyStateCreateInfo iasci={VK_STRUCTURE_TYPE_PIPELINE_INPUT_ASSEMBLY_STATE_CREATE_INFO,0,0,VK_PRIMITIVE_TOPOLOGY_TRIANGLE_LIST,VK_FALSE};
    VkViewport vp={0,0,WIDTH,HEIGHT,0,1};
    VkRect2D sc={{0,0},{WIDTH,HEIGHT}};
    VkPipelineViewportStateCreateInfo vpsci={VK_STRUCTURE_TYPE_PIPELINE_VIEWPORT_STATE_CREATE_INFO,0,0,1,&vp,1,&sc};
    VkPipelineRasterizationStateCreateInfo rsci={VK_STRUCTURE_TYPE_PIPELINE_RASTERIZATION_STATE_CREATE_INFO,0,0,0,0,VK_POLYGON_MODE_FILL,0,VK_FRONT_FACE_CLOCKWISE,0,0,0,0,1.0f};
    VkPipelineMultisampleStateCreateInfo msci={VK_STRUCTURE_TYPE_PIPELINE_MULTISAMPLE_STATE_CREATE_INFO,0,0,VK_SAMPLE_COUNT_1_BIT,0,0,0,0};
    VkPipelineColorBlendAttachmentState cba={0,0,0,0,0,0,0,VK_COLOR_COMPONENT_R_BIT|VK_COLOR_COMPONENT_G_BIT|VK_COLOR_COMPONENT_B_BIT|VK_COLOR_COMPONENT_A_BIT};
    VkPipelineColorBlendStateCreateInfo cbsci={VK_STRUCTURE_TYPE_PIPELINE_COLOR_BLEND_STATE_CREATE_INFO,0,0,0,0,1,&cba,{0,0,0,0}};

    VkAttachmentDescription att={0,VK_FORMAT_R8G8B8A8_UNORM,VK_SAMPLE_COUNT_1_BIT,VK_ATTACHMENT_LOAD_OP_CLEAR,VK_ATTACHMENT_STORE_OP_STORE,0,0,VK_IMAGE_LAYOUT_UNDEFINED,VK_IMAGE_LAYOUT_GENERAL};
    VkAttachmentReference ar={0,VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL};
    VkSubpassDescription sp={0,VK_PIPELINE_BIND_POINT_GRAPHICS,0,0,1,&ar,0,0,0,0};
    VkRenderPassCreateInfo rpci={VK_STRUCTURE_TYPE_RENDER_PASS_CREATE_INFO,0,0,1,&att,1,&sp,0,0};
    VkRenderPass rp; pvkCreateRenderPass(dev,&rpci,0,&rp); INC(C_create_rp);

    VkPipelineShaderStageCreateInfo stages_a[2]={
        {VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,0,0,VK_SHADER_STAGE_VERTEX_BIT,vs,"main",0},
        {VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,0,0,VK_SHADER_STAGE_FRAGMENT_BIT,fs_a,"main",0},
    };
    VkPipelineShaderStageCreateInfo stages_b[2]={
        {VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,0,0,VK_SHADER_STAGE_VERTEX_BIT,vs,"main",0},
        {VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,0,0,VK_SHADER_STAGE_FRAGMENT_BIT,fs_b,"main",0},
    };
    VkGraphicsPipelineCreateInfo gpci={VK_STRUCTURE_TYPE_GRAPHICS_PIPELINE_CREATE_INFO,0,0,2,stages_a,&vici,&iasci,0,&vpsci,&rsci,&msci,0,&cbsci,0,pl,rp,0,0,0};
    VkPipeline pipe_a; pvkCreateGraphicsPipelines(dev,0,1,&gpci,0,&pipe_a); INC(C_create_pipe);
    gpci.pStages=stages_b;
    VkPipeline pipe_b; pvkCreateGraphicsPipelines(dev,0,1,&gpci,0,&pipe_b); INC(C_create_pipe);

    /* ---- Render target ---- */
    VkImageCreateInfo imci={VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO,0,0,VK_IMAGE_TYPE_2D,VK_FORMAT_R8G8B8A8_UNORM,{WIDTH,HEIGHT,1},1,1,VK_SAMPLE_COUNT_1_BIT,VK_IMAGE_TILING_OPTIMAL,VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT|VK_IMAGE_USAGE_TRANSFER_SRC_BIT,VK_SHARING_MODE_EXCLUSIVE,0,0,VK_IMAGE_LAYOUT_UNDEFINED};
    VkImage img; pvkCreateImage(dev,&imci,0,&img); INC(C_create_img);
    { VkMemoryRequirements mr; pvkGetImageMemoryRequirements(dev,img,&mr); INC(C_get_img_mem_req);
      VkMemoryAllocateInfo mai={VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,0,mr.size,0};
      VkDeviceMemory m; pvkAllocateMemory(dev,&mai,0,&m); INC(C_alloc_mem); pvkBindImageMemory(dev,img,m,0); INC(C_bind_img_mem); }
    VkImageViewCreateInfo ivci={VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO,0,0,img,VK_IMAGE_VIEW_TYPE_2D,VK_FORMAT_R8G8B8A8_UNORM,{VK_COMPONENT_SWIZZLE_IDENTITY,VK_COMPONENT_SWIZZLE_IDENTITY,VK_COMPONENT_SWIZZLE_IDENTITY,VK_COMPONENT_SWIZZLE_IDENTITY},{VK_IMAGE_ASPECT_COLOR_BIT,0,1,0,1}};
    VkImageView iv; pvkCreateImageView(dev,&ivci,0,&iv); INC(C_create_iv);
    VkFramebufferCreateInfo fci={VK_STRUCTURE_TYPE_FRAMEBUFFER_CREATE_INFO,0,0,rp,1,&iv,WIDTH,HEIGHT,1};
    VkFramebuffer fb; pvkCreateFramebuffer(dev,&fci,0,&fb); INC(C_create_fb);

    /* ---- Command buffer ---- */
    VkCommandPoolCreateInfo cpi={VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO,0,0,qf};
    VkCommandPool cp; pvkCreateCommandPool(dev,&cpi,0,&cp);
    VkCommandBufferAllocateInfo cbai={VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO,0,cp,VK_COMMAND_BUFFER_LEVEL_PRIMARY,1};
    VkCommandBuffer cb; pvkAllocateCommandBuffers(dev,&cbai,&cb); INC(C_alloc_cb);
    VkFenceCreateInfo fnci={VK_STRUCTURE_TYPE_FENCE_CREATE_INFO,0,0};
    VkFence fence; pvkCreateFence(dev,&fnci,0,&fence);

    /* ---- Vertex/index buffers ---- */
    float verts[]={0,0, 1,0, 0,1, 1,1};
    uint16_t indices[]={0,1,2, 1,3,2};
    VkBuffer vb,ib; VkDeviceMemory vbm,ibm;
    { VkBufferCreateInfo bci={VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO,0,0,sizeof(verts),VK_BUFFER_USAGE_VERTEX_BUFFER_BIT,VK_SHARING_MODE_EXCLUSIVE,0,0};
      pvkCreateBuffer(dev,&bci,0,&vb); INC(C_create_buf);
      VkMemoryRequirements mr; pvkGetBufferMemoryRequirements(dev,vb,&mr); INC(C_get_img_mem_req);
      VkMemoryAllocateInfo mai={VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,0,mr.size,0};
      pvkAllocateMemory(dev,&mai,0,&vbm); INC(C_alloc_mem); pvkBindBufferMemory(dev,vb,vbm,0);
      void *p; pvkMapMemory(dev,vbm,0,sizeof(verts),0,&p); INC(C_map); memcpy(p,verts,sizeof(verts)); pvkUnmapMemory(dev,vbm); INC(C_unmap); }
    { VkBufferCreateInfo bci={VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO,0,0,sizeof(indices),VK_BUFFER_USAGE_INDEX_BUFFER_BIT,VK_SHARING_MODE_EXCLUSIVE,0,0};
      pvkCreateBuffer(dev,&bci,0,&ib); INC(C_create_buf);
      VkMemoryRequirements mr; pvkGetBufferMemoryRequirements(dev,ib,&mr); INC(C_get_img_mem_req);
      VkMemoryAllocateInfo mai={VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,0,mr.size,0};
      pvkAllocateMemory(dev,&mai,0,&ibm); INC(C_alloc_mem); pvkBindBufferMemory(dev,ib,ibm,0);
      void *p; pvkMapMemory(dev,ibm,0,sizeof(indices),0,&p); INC(C_map); memcpy(p,indices,sizeof(indices)); pvkUnmapMemory(dev,ibm); INC(C_unmap); }

    /* ============ BENCHMARK LOOP ============ */
    VkDeviceSize off=0;
    double t0 = now_ms();

    LOG("=== DXVK sim: %d frm x %d draws, pipeline swap every %d, UBO every %d ===",
        NUM_FRAMES, DRAWS_PER_FRAME, PIPELINE_SWITCH_EVERY, UBO_BIND_EVERY);

    VkPipeline current_pipe = pipe_a;

    for (int frame = 0; frame < NUM_FRAMES; frame++) {
        /* Update UBO data for this frame */
        for (int i = 0; i < 16; i++) ubo_data[i] = (float)(frame * 0.01f + i * 0.001f);
        void *p; pvkMapMemory(dev,ubo_mem,0,sizeof(ubo_data),0,&p); INC(C_map);
        memcpy(p,ubo_data,sizeof(ubo_data)); pvkUnmapMemory(dev,ubo_mem); INC(C_unmap);
        INC(C_update_ubo);

        pvkResetCommandBuffer(cb,0); INC(C_reset_cb);
        VkCommandBufferBeginInfo cbbi={VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO,0,VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT,0};
        pvkBeginCommandBuffer(cb,&cbbi); INC(C_begin_cb);

        VkClearValue clr={{{0,0,0,1}}};
        VkRenderPassBeginInfo rpbi={VK_STRUCTURE_TYPE_RENDER_PASS_BEGIN_INFO,0,rp,fb,{{0,0},{WIDTH,HEIGHT}},1,&clr};
        pvkCmdBeginRenderPass(cb,&rpbi,VK_SUBPASS_CONTENTS_INLINE); INC(C_begin_rp);
        pvkCmdSetViewport(cb,0,1,&vp); INC(C_set_vp);
        pvkCmdSetScissor(cb,0,1,&sc); INC(C_set_sc);
        pvkCmdBindPipeline(cb,VK_PIPELINE_BIND_POINT_GRAPHICS,pipe_a); INC(C_bind_pipe);
        pvkCmdBindVertexBuffers(cb,0,1,&vb,&off); INC(C_bind_vb);
        pvkCmdBindIndexBuffer(cb,ib,0,VK_INDEX_TYPE_UINT16); INC(C_bind_ib);

        for (int d = 0; d < DRAWS_PER_FRAME; d++) {
            /* Pipeline switch: unlike DXVK's full commitGraphicsState on switch */
            if ((d % PIPELINE_SWITCH_EVERY) == 0 && d > 0) {
                current_pipe = (current_pipe == pipe_a) ? pipe_b : pipe_a;
                pvkCmdBindPipeline(cb, VK_PIPELINE_BIND_POINT_GRAPHICS, current_pipe);
                INC(C_bind_pipe);
            }

            /* Descriptor set update: bind UBO with new offset */
            if ((d % UBO_BIND_EVERY) == 0) {
                VkDescriptorBufferInfo dbi={ubo,0,sizeof(ubo_data)};
                VkWriteDescriptorSet wds={VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET,0,ds,0,0,1,
                    VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER,0,&dbi,0};
                pvkUpdateDescriptorSets(dev,1,&wds,0,0); INC(C_update_ds);
                /* DXVK would also cmdBindDescriptorSets here */
            }

            float a = (frame*DRAWS_PER_FRAME+d)*0.01f;
            float pc[8]={cosf(a),-sinf(a),0,0, sinf(a),cosf(a),0,0};
            pvkCmdPushConstants(cb,pl,VK_SHADER_STAGE_VERTEX_BIT,0,32,pc); INC(C_push);
            pvkCmdDrawIndexed(cb,6,1,0,0,0); INC(C_draw);
        }

        pvkCmdEndRenderPass(cb); INC(C_end_rp);
        pvkEndCommandBuffer(cb); INC(C_end_cb);
        pvkResetFences(dev,1,&fence); INC(C_reset_fence);
        VkSubmitInfo si={VK_STRUCTURE_TYPE_SUBMIT_INFO,0,0,0,0,1,&cb,0,0};
        pvkQueueSubmit(q,1,&si,fence); INC(C_submit);
        pvkWaitForFences(dev,1,&fence,VK_TRUE,UINT64_MAX); INC(C_wait_fence);
    }

    double t1=now_ms(), elapsed=t1-t0;
    int total_draws = NUM_FRAMES * DRAWS_PER_FRAME;
    LOG("Time: %.1f ms  FPS: %.1f  draws/sec: %.0f  ms/draw: %.3f",
        elapsed, NUM_FRAMES*1000.0/elapsed, (double)total_draws*1000.0/elapsed,
        elapsed/total_draws);

    /* === vkCmd-level call counters === */
    LOG("\n--- vkCmd call counters ---");
    LOG("%-22s %s", "CALL", "COUNT");
    for (int i = 0; i < C_COUNT; i++) {
        if (counters[i])
            LOG("  %-20s %u", counter_names[i], counters[i]);
    }

    /* teardown */
    pvkDestroyFence(dev,fence,0); pvkDestroyCommandPool(dev,cp,0);
    pvkDestroyFramebuffer(dev,fb,0); pvkDestroyImageView(dev,iv,0); pvkDestroyImage(dev,img,0);
    pvkDestroyPipeline(dev,pipe_a,0); pvkDestroyPipeline(dev,pipe_b,0);
    pvkDestroyPipelineLayout(dev,pl,0); pvkDestroyRenderPass(dev,rp,0);
    pvkDestroyShaderModule(dev,vs,0); pvkDestroyShaderModule(dev,fs_a,0); pvkDestroyShaderModule(dev,fs_b,0);
    pvkDestroyDescriptorPool(dev,dpool,0); pvkDestroyDescriptorSetLayout(dev,dsl,0);
    pvkFreeMemory(dev,ubo_mem,0); pvkDestroyBuffer(dev,ubo,0);
    pvkFreeMemory(dev,vbm,0); pvkDestroyBuffer(dev,vb,0);
    pvkFreeMemory(dev,ibm,0); pvkDestroyBuffer(dev,ib,0);
    pDD(dev,0); pDI(inst,0); dlclose(icd);
    LOG("done"); return 0;
}
