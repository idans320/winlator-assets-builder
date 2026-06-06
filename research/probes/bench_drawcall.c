#include <dlfcn.h>
#include <vulkan/vulkan.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <math.h>
#include "cube_spirv.h"

#define WIDTH  512
#define HEIGHT 512
#define NUM_FRAMES 50
#define DRAWS_PER_FRAME 5000

static double now_ms(void) {
    struct timespec ts; clock_gettime(CLOCK_MONOTONIC, &ts);
    return ts.tv_sec * 1000.0 + ts.tv_nsec / 1000000.0;
}
#define LOG(fmt,...) do{ fprintf(stderr, fmt "\n", ##__VA_ARGS__); fflush(stderr); }while(0)

int main(int argc, char **argv) {
    int coalesce = (argc > 1 && argv[1][0] == 'c');
    LOG(coalesce ? "=== COALESCED ===" : "=== BASELINE ===");

    void *icd = dlopen("/data/local/tmp/libvulkan_freedreno.so", RTLD_NOW);
    if (!icd) { LOG("dlopen fail"); return 1; }

    PFN_vkVoidFunction (*gp)(VkInstance,const char*) =
        (PFN_vkVoidFunction(*)(VkInstance,const char*)) dlsym(icd, "vk_icdGetInstanceProcAddr");
    if (!gp) { LOG("no gp"); return 1; }

    PFN_vkCreateInstance pCreateInstance = (PFN_vkCreateInstance)gp(NULL,"vkCreateInstance");
    VkApplicationInfo ai={VK_STRUCTURE_TYPE_APPLICATION_INFO,0,"bench",1,0,VK_API_VERSION_1_2};
    VkInstanceCreateInfo ici={VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO,0,0,&ai,0,0,0,0};
    VkInstance inst=0; pCreateInstance(&ici,0,&inst);
    if(!inst){LOG("no inst");return 1;}

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
    const char *devext[] = { "VK_EXT_extended_dynamic_state" };
    VkDeviceCreateInfo dci={VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO,0,0,1,&qci,0,0,1,devext,0};
    VkDevice dev=0; pCD(pd,&dci,0,&dev);
    VkQueue q; pGQ(dev,qf,0,&q);

    #define GET(fn,name) PFN_##fn p##fn=(PFN_##fn)gp(inst,name)
    GET(vkCreateShaderModule,"vkCreateShaderModule");
    GET(vkDestroyShaderModule,"vkDestroyShaderModule");
    GET(vkCreatePipelineLayout,"vkCreatePipelineLayout");
    GET(vkDestroyPipelineLayout,"vkDestroyPipelineLayout");
    GET(vkCreateGraphicsPipelines,"vkCreateGraphicsPipelines");
    GET(vkDestroyPipeline,"vkDestroyPipeline");
    GET(vkCreateRenderPass,"vkCreateRenderPass");
    GET(vkDestroyRenderPass,"vkDestroyRenderPass");
    GET(vkCmdBeginRenderPass,"vkCmdBeginRenderPass");
    GET(vkCmdEndRenderPass,"vkCmdEndRenderPass");
    GET(vkCmdBindPipeline,"vkCmdBindPipeline");
    GET(vkCmdSetViewport,"vkCmdSetViewport");
    GET(vkCmdSetScissor,"vkCmdSetScissor");
    GET(vkCreateImage,"vkCreateImage");GET(vkDestroyImage,"vkDestroyImage");
    GET(vkGetImageMemoryRequirements,"vkGetImageMemoryRequirements");
    GET(vkAllocateMemory,"vkAllocateMemory");GET(vkFreeMemory,"vkFreeMemory");
    GET(vkBindImageMemory,"vkBindImageMemory");
    GET(vkCreateImageView,"vkCreateImageView");GET(vkDestroyImageView,"vkDestroyImageView");
    GET(vkCreateFramebuffer,"vkCreateFramebuffer");GET(vkDestroyFramebuffer,"vkDestroyFramebuffer");
    GET(vkCreateCommandPool,"vkCreateCommandPool");GET(vkDestroyCommandPool,"vkDestroyCommandPool");
    GET(vkAllocateCommandBuffers,"vkAllocateCommandBuffers");
    GET(vkBeginCommandBuffer,"vkBeginCommandBuffer");GET(vkEndCommandBuffer,"vkEndCommandBuffer");
    GET(vkQueueSubmit,"vkQueueSubmit");GET(vkResetCommandBuffer,"vkResetCommandBuffer");
    GET(vkCreateFence,"vkCreateFence");GET(vkDestroyFence,"vkDestroyFence");
    GET(vkWaitForFences,"vkWaitForFences");GET(vkResetFences,"vkResetFences");
    GET(vkCmdBindVertexBuffers,"vkCmdBindVertexBuffers");
    GET(vkCmdPushConstants,"vkCmdPushConstants");
    GET(vkCmdBindIndexBuffer,"vkCmdBindIndexBuffer");GET(vkCmdDrawIndexed,"vkCmdDrawIndexed");
    GET(vkCmdSetDepthTestEnable,"vkCmdSetDepthTestEnable");
    GET(vkCreateBuffer,"vkCreateBuffer");GET(vkDestroyBuffer,"vkDestroyBuffer");
    GET(vkGetBufferMemoryRequirements,"vkGetBufferMemoryRequirements");
    GET(vkBindBufferMemory,"vkBindBufferMemory");
    GET(vkMapMemory,"vkMapMemory");GET(vkUnmapMemory,"vkUnmapMemory");
    #undef GET

    /* shaders */
    VkShaderModuleCreateInfo smci={VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO,0,0,vs_spv_size,vs_spv};
    VkShaderModule vs; pvkCreateShaderModule(dev,&smci,0,&vs);
    smci.pCode=fs_spv; smci.codeSize=fs_spv_size;
    VkShaderModule fs; pvkCreateShaderModule(dev,&smci,0,&fs);

    /* tiny quad geometry */
    float verts[]={0,0,0,1,0,0, 1,0,0,0,1,0, 0,1,0,0,0,1, 1,1,0,1,1,0};
    uint16_t indices[]={0,1,2, 1,3,2};
    VkBuffer vb,ib; VkDeviceMemory vbm,ibm;
    { VkBufferCreateInfo bci={VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO,0,0,sizeof(verts),VK_BUFFER_USAGE_VERTEX_BUFFER_BIT,VK_SHARING_MODE_EXCLUSIVE,0,0};
      pvkCreateBuffer(dev,&bci,0,&vb);
      VkMemoryRequirements mr; pvkGetBufferMemoryRequirements(dev,vb,&mr);
      VkMemoryAllocateInfo mai={VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,0,mr.size,0};
      pvkAllocateMemory(dev,&mai,0,&vbm); pvkBindBufferMemory(dev,vb,vbm,0);
      void *p; pvkMapMemory(dev,vbm,0,sizeof(verts),0,&p); memcpy(p,verts,sizeof(verts)); pvkUnmapMemory(dev,vbm); }
    { VkBufferCreateInfo bci={VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO,0,0,sizeof(indices),VK_BUFFER_USAGE_INDEX_BUFFER_BIT,VK_SHARING_MODE_EXCLUSIVE,0,0};
      pvkCreateBuffer(dev,&bci,0,&ib);
      VkMemoryRequirements mr; pvkGetBufferMemoryRequirements(dev,ib,&mr);
      VkMemoryAllocateInfo mai={VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,0,mr.size,0};
      pvkAllocateMemory(dev,&mai,0,&ibm); pvkBindBufferMemory(dev,ib,ibm,0);
      void *p; pvkMapMemory(dev,ibm,0,sizeof(indices),0,&p); memcpy(p,indices,sizeof(indices)); pvkUnmapMemory(dev,ibm); }

    VkVertexInputBindingDescription vib={0,24,VK_VERTEX_INPUT_RATE_VERTEX};
    VkVertexInputAttributeDescription via[2]={{0,0,VK_FORMAT_R32G32B32_SFLOAT,0},{1,0,VK_FORMAT_R32G32B32_SFLOAT,12}};
    VkPipelineVertexInputStateCreateInfo vici={VK_STRUCTURE_TYPE_PIPELINE_VERTEX_INPUT_STATE_CREATE_INFO,0,0,1,&vib,2,via};
    VkPipelineInputAssemblyStateCreateInfo iasci={VK_STRUCTURE_TYPE_PIPELINE_INPUT_ASSEMBLY_STATE_CREATE_INFO,0,0,VK_PRIMITIVE_TOPOLOGY_TRIANGLE_LIST,VK_FALSE};
    VkPipelineShaderStageCreateInfo stages[2]={
        {VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,0,0,VK_SHADER_STAGE_VERTEX_BIT,vs,"main",0},
        {VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,0,0,VK_SHADER_STAGE_FRAGMENT_BIT,fs,"main",0},
    };
    VkViewport vp={0,0,WIDTH,HEIGHT,0,1};
    VkRect2D sc={{0,0},{WIDTH,HEIGHT}};
    VkPipelineViewportStateCreateInfo vpsci={VK_STRUCTURE_TYPE_PIPELINE_VIEWPORT_STATE_CREATE_INFO,0,0,1,&vp,1,&sc};
    VkPipelineRasterizationStateCreateInfo rsci={VK_STRUCTURE_TYPE_PIPELINE_RASTERIZATION_STATE_CREATE_INFO,0,0,0,0,VK_POLYGON_MODE_FILL,0,VK_FRONT_FACE_CLOCKWISE,0,0,0,0,1.0f};
    VkPipelineMultisampleStateCreateInfo msci={VK_STRUCTURE_TYPE_PIPELINE_MULTISAMPLE_STATE_CREATE_INFO,0,0,VK_SAMPLE_COUNT_1_BIT,0,0,0,0};
    VkPipelineColorBlendAttachmentState cba={0,0,0,0,0,0,0,VK_COLOR_COMPONENT_R_BIT|VK_COLOR_COMPONENT_G_BIT|VK_COLOR_COMPONENT_B_BIT|VK_COLOR_COMPONENT_A_BIT};
    VkPipelineColorBlendStateCreateInfo cbsci={VK_STRUCTURE_TYPE_PIPELINE_COLOR_BLEND_STATE_CREATE_INFO,0,0,0,0,1,&cba,{0,0,0,0}};
    VkPushConstantRange pcr={VK_SHADER_STAGE_VERTEX_BIT,0,32};
    VkPipelineLayoutCreateInfo plci={VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO,0,0,0,0,1,&pcr};
    VkPipelineLayout pl; pvkCreatePipelineLayout(dev,&plci,0,&pl);
    VkAttachmentDescription att={0,VK_FORMAT_R8G8B8A8_UNORM,VK_SAMPLE_COUNT_1_BIT,VK_ATTACHMENT_LOAD_OP_CLEAR,VK_ATTACHMENT_STORE_OP_STORE,0,0,VK_IMAGE_LAYOUT_UNDEFINED,VK_IMAGE_LAYOUT_GENERAL};
    VkAttachmentReference ar={0,VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL};
    VkSubpassDescription spd={0,VK_PIPELINE_BIND_POINT_GRAPHICS,0,0,1,&ar,0,0,0,0};
    VkRenderPassCreateInfo rpci={VK_STRUCTURE_TYPE_RENDER_PASS_CREATE_INFO,0,0,1,&att,1,&spd,0,0};
    VkRenderPass rp; pvkCreateRenderPass(dev,&rpci,0,&rp);
    VkGraphicsPipelineCreateInfo gpci={VK_STRUCTURE_TYPE_GRAPHICS_PIPELINE_CREATE_INFO,0,0,2,stages,&vici,&iasci,0,&vpsci,&rsci,&msci,0,&cbsci,0,pl,rp,0,0,0};
    VkPipeline pipe; pvkCreateGraphicsPipelines(dev,0,1,&gpci,0,&pipe);

    VkImageCreateInfo imci={VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO,0,0,VK_IMAGE_TYPE_2D,VK_FORMAT_R8G8B8A8_UNORM,{WIDTH,HEIGHT,1},1,1,VK_SAMPLE_COUNT_1_BIT,VK_IMAGE_TILING_OPTIMAL,VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT|VK_IMAGE_USAGE_TRANSFER_SRC_BIT,VK_SHARING_MODE_EXCLUSIVE,0,0,VK_IMAGE_LAYOUT_UNDEFINED};
    VkImage img; pvkCreateImage(dev,&imci,0,&img); VkMemoryRequirements mri; pvkGetImageMemoryRequirements(dev,img,&mri);
    VkMemoryAllocateInfo mai={VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,0,mri.size,0};
    VkDeviceMemory imem; pvkAllocateMemory(dev,&mai,0,&imem); pvkBindImageMemory(dev,img,imem,0);
    VkImageViewCreateInfo ivci={VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO,0,0,img,VK_IMAGE_VIEW_TYPE_2D,VK_FORMAT_R8G8B8A8_UNORM,{VK_COMPONENT_SWIZZLE_IDENTITY,VK_COMPONENT_SWIZZLE_IDENTITY,VK_COMPONENT_SWIZZLE_IDENTITY,VK_COMPONENT_SWIZZLE_IDENTITY},{VK_IMAGE_ASPECT_COLOR_BIT,0,1,0,1}};
    VkImageView iv; pvkCreateImageView(dev,&ivci,0,&iv);
    VkFramebufferCreateInfo fci={VK_STRUCTURE_TYPE_FRAMEBUFFER_CREATE_INFO,0,0,rp,1,&iv,WIDTH,HEIGHT,1};
    VkFramebuffer fb; pvkCreateFramebuffer(dev,&fci,0,&fb);

    VkCommandPoolCreateInfo cpi={VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO,0,0,qf};
    VkCommandPool cp; pvkCreateCommandPool(dev,&cpi,0,&cp);
    VkCommandBufferAllocateInfo cbai={VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO,0,cp,VK_COMMAND_BUFFER_LEVEL_PRIMARY,1};
    VkCommandBuffer cb; pvkAllocateCommandBuffers(dev,&cbai,&cb);
    VkFenceCreateInfo fnci={VK_STRUCTURE_TYPE_FENCE_CREATE_INFO,0,0};
    VkFence fence; pvkCreateFence(dev,&fnci,0,&fence);

    LOG("=== Bench: %d frames, %d draws/frame ===", NUM_FRAMES, DRAWS_PER_FRAME);
    VkDeviceSize off=0;
    double t0 = now_ms();

    for (int frame = 0; frame < NUM_FRAMES; frame++) {
        pvkResetCommandBuffer(cb,0);
        VkCommandBufferBeginInfo cbbi={VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO,0,VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT,0};
        pvkBeginCommandBuffer(cb,&cbbi);
        VkClearValue clr={{{0,0,0,1}}};
        VkRenderPassBeginInfo rpbi={VK_STRUCTURE_TYPE_RENDER_PASS_BEGIN_INFO,0,rp,fb,{{0,0},{WIDTH,HEIGHT}},1,&clr};
        pvkCmdBeginRenderPass(cb,&rpbi,VK_SUBPASS_CONTENTS_INLINE);
        pvkCmdSetViewport(cb,0,1,&vp); pvkCmdSetScissor(cb,0,1,&sc);
        pvkCmdBindPipeline(cb,VK_PIPELINE_BIND_POINT_GRAPHICS,pipe);
        pvkCmdBindVertexBuffers(cb,0,1,&vb,&off);
        pvkCmdBindIndexBuffer(cb,ib,0,VK_INDEX_TYPE_UINT16);

        for (int d = 0; d < DRAWS_PER_FRAME; d++) {
            pvkCmdSetDepthTestEnable(cb, d & 1);
            float x = (d % 100) * 0.01f;
            float y = (d / 100) * 0.01f;
            float a = (frame * DRAWS_PER_FRAME + d) * 0.001f;
            float m2[8] = { cosf(a), -sinf(a), x, 0, sinf(a), cosf(a), y, 0 };
            pvkCmdPushConstants(cb, pl, VK_SHADER_STAGE_VERTEX_BIT, 0, 32, m2);
            pvkCmdDrawIndexed(cb, 6, 1, 0, 0, 0);
        }

        pvkCmdEndRenderPass(cb); pvkEndCommandBuffer(cb);
        pvkResetFences(dev,1,&fence);
        VkSubmitInfo si={VK_STRUCTURE_TYPE_SUBMIT_INFO,0,0,0,0,1,&cb,0,0};
        pvkQueueSubmit(q,1,&si,fence);
        pvkWaitForFences(dev,1,&fence,VK_TRUE,UINT64_MAX);
    }

    double t1=now_ms(), elapsed=t1-t0;
    double draws = (double)NUM_FRAMES * DRAWS_PER_FRAME;
    LOG("Time: %.1f ms  FPS: %.1f  draws/sec: %.0f  ms/draw: %.4f",
        elapsed, NUM_FRAMES*1000.0/elapsed, draws*1000.0/elapsed, elapsed/draws);

    /* teardown */
    pvkDestroyFence(dev,fence,0); pvkDestroyCommandPool(dev,cp,0);
    pvkDestroyFramebuffer(dev,fb,0); pvkDestroyImageView(dev,iv,0); pvkDestroyImage(dev,img,0); pvkFreeMemory(dev,imem,0);
    pvkDestroyPipeline(dev,pipe,0); pvkDestroyPipelineLayout(dev,pl,0); pvkDestroyRenderPass(dev,rp,0);
    pvkDestroyShaderModule(dev,vs,0); pvkDestroyShaderModule(dev,fs,0);
    pvkFreeMemory(dev,vbm,0); pvkDestroyBuffer(dev,vb,0);
    pvkFreeMemory(dev,ibm,0); pvkDestroyBuffer(dev,ib,0);
    pDD(dev,0); pDI(inst,0); dlclose(icd);
    LOG("done"); return 0;
}
