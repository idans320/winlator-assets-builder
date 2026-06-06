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
#define NUM_FRAMES 300

static double now_ms(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return ts.tv_sec * 1000.0 + ts.tv_nsec / 1000000.0;
}

#define LOG(fmt,...) do{ fprintf(stderr, fmt "\n", ##__VA_ARGS__); fflush(stderr); }while(0)

int main(void) {
    void *icd = dlopen("/data/local/tmp/libvulkan_freedreno.so", RTLD_NOW);
    if (!icd) { LOG("dlopen fail"); return 1; }

    PFN_vkVoidFunction (*gp)(VkInstance,const char*) =
        (PFN_vkVoidFunction(*)(VkInstance,const char*))
        dlsym(icd, "vk_icdGetInstanceProcAddr");
    if (!gp) { LOG("no gp"); return 1; }

    PFN_vkCreateInstance pCreateInstance = (PFN_vkCreateInstance)gp(NULL,"vkCreateInstance");
    VkApplicationInfo ai={VK_STRUCTURE_TYPE_APPLICATION_INFO,0,"c",1,0,VK_API_VERSION_1_2};
    VkInstanceCreateInfo ici={VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO,0,0,&ai,0,0,0,0};
    VkInstance inst=0;
    pCreateInstance(&ici,0,&inst);
    if(!inst){LOG("no inst");return 1;}

    PFN_vkDestroyInstance pDestroyInstance=(PFN_vkDestroyInstance)gp(inst,"vkDestroyInstance");
    PFN_vkEnumeratePhysicalDevices pEnumDevs=(PFN_vkEnumeratePhysicalDevices)gp(inst,"vkEnumeratePhysicalDevices");
    PFN_vkGetPhysicalDeviceProperties pGetProps=(PFN_vkGetPhysicalDeviceProperties)gp(inst,"vkGetPhysicalDeviceProperties");
    PFN_vkGetPhysicalDeviceQueueFamilyProperties pGetQFP=(PFN_vkGetPhysicalDeviceQueueFamilyProperties)gp(inst,"vkGetPhysicalDeviceQueueFamilyProperties");
    PFN_vkCreateDevice pCreateDevice=(PFN_vkCreateDevice)gp(inst,"vkCreateDevice");
    PFN_vkDestroyDevice pDestroyDevice=(PFN_vkDestroyDevice)gp(inst,"vkDestroyDevice");
    PFN_vkGetDeviceQueue pGetQueue=(PFN_vkGetDeviceQueue)gp(inst,"vkGetDeviceQueue");

    uint32_t nd=0; pEnumDevs(inst,&nd,0);
    VkPhysicalDevice pd; pEnumDevs(inst,&nd,&pd);
    VkPhysicalDeviceProperties pr; pGetProps(pd,&pr);
    LOG("GPU: %s", pr.deviceName);

    uint32_t qc=0; pGetQFP(pd,&qc,0);
    VkQueueFamilyProperties qp[4]; pGetQFP(pd,&qc,qp);
    uint32_t qf=UINT32_MAX;
    for(uint32_t i=0;i<qc&&i<4;i++) if(qp[i].queueFlags&VK_QUEUE_GRAPHICS_BIT){qf=i;break;}
    if(qf==UINT32_MAX)return 1;
    float prio=1.0f;
    VkDeviceQueueCreateInfo qci={VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO,0,0,qf,1,&prio};
    VkDeviceCreateInfo dci={VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO,0,0,1,&qci,0,0,0,0,0};
    VkDevice dev=0;
    pCreateDevice(pd,&dci,0,&dev);
    VkQueue q; pGetQueue(dev,qf,0,&q);

    #define GET(type,name) PFN_##type p##type=(PFN_##type)gp(inst,name)
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
    GET(vkCreateImage,"vkCreateImage");
    GET(vkDestroyImage,"vkDestroyImage");
    GET(vkGetImageMemoryRequirements,"vkGetImageMemoryRequirements");
    GET(vkAllocateMemory,"vkAllocateMemory");
    GET(vkFreeMemory,"vkFreeMemory");
    GET(vkBindImageMemory,"vkBindImageMemory");
    GET(vkCreateImageView,"vkCreateImageView");
    GET(vkDestroyImageView,"vkDestroyImageView");
    GET(vkCreateFramebuffer,"vkCreateFramebuffer");
    GET(vkDestroyFramebuffer,"vkDestroyFramebuffer");
    GET(vkCreateCommandPool,"vkCreateCommandPool");
    GET(vkDestroyCommandPool,"vkDestroyCommandPool");
    GET(vkAllocateCommandBuffers,"vkAllocateCommandBuffers");
    GET(vkBeginCommandBuffer,"vkBeginCommandBuffer");
    GET(vkEndCommandBuffer,"vkEndCommandBuffer");
    GET(vkQueueSubmit,"vkQueueSubmit");
    GET(vkResetCommandBuffer,"vkResetCommandBuffer");
    GET(vkCreateFence,"vkCreateFence");
    GET(vkDestroyFence,"vkDestroyFence");
    GET(vkWaitForFences,"vkWaitForFences");
    GET(vkResetFences,"vkResetFences");
    GET(vkCmdBindVertexBuffers,"vkCmdBindVertexBuffers");
    GET(vkCmdPushConstants,"vkCmdPushConstants");
    GET(vkCmdBindIndexBuffer,"vkCmdBindIndexBuffer");
    GET(vkCmdDrawIndexed,"vkCmdDrawIndexed");
    GET(vkCreateBuffer,"vkCreateBuffer");
    GET(vkDestroyBuffer,"vkDestroyBuffer");
    GET(vkGetBufferMemoryRequirements,"vkGetBufferMemoryRequirements");
    GET(vkBindBufferMemory,"vkBindBufferMemory");
    GET(vkMapMemory,"vkMapMemory");
    GET(vkUnmapMemory,"vkUnmapMemory");
    #undef GET

    /* ---- shaders ---- */
    VkShaderModuleCreateInfo smci={VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO,0,0,sizeof(vs_spv),vs_spv};
    VkShaderModule vs; pvkCreateShaderModule(dev,&smci,0,&vs);
    smci.pCode=fs_spv; smci.codeSize=sizeof(fs_spv);
    VkShaderModule fs; pvkCreateShaderModule(dev,&smci,0,&fs);

    /* ---- cube geometry ---- */
    float verts[]={
        -0.5f,-0.5f,-0.5f, 1,0,0,   0.5f,-0.5f,-0.5f, 0,1,0,
        -0.5f, 0.5f,-0.5f, 0,0,1,   0.5f, 0.5f,-0.5f, 1,1,0,
        -0.5f,-0.5f, 0.5f, 1,0,1,   0.5f,-0.5f, 0.5f, 0,1,1,
        -0.5f, 0.5f, 0.5f, 1,1,1,   0.5f, 0.5f, 0.5f, 0,0,0,
    };
    uint16_t indices[]={
        0,1,2, 1,3,2, 4,5,6, 5,7,6, 0,4,1, 4,5,1,
        2,3,6, 3,7,6, 0,2,4, 2,6,4, 1,5,3, 5,7,3,
    };

    VkBuffer vb,ib; VkDeviceMemory vbm,ibm;
    VkBufferCreateInfo bci={VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO,0,0,sizeof(verts),VK_BUFFER_USAGE_VERTEX_BUFFER_BIT,VK_SHARING_MODE_EXCLUSIVE,0,0};
    pvkCreateBuffer(dev,&bci,0,&vb);
    { VkMemoryRequirements mr; pvkGetBufferMemoryRequirements(dev,vb,&mr);
      VkMemoryAllocateInfo mai={VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,0,mr.size,0};
      pvkAllocateMemory(dev,&mai,0,&vbm); pvkBindBufferMemory(dev,vb,vbm,0); }
    bci.size=sizeof(indices); bci.usage=VK_BUFFER_USAGE_INDEX_BUFFER_BIT;
    pvkCreateBuffer(dev,&bci,0,&ib);
    { VkMemoryRequirements mr; pvkGetBufferMemoryRequirements(dev,ib,&mr);
      VkMemoryAllocateInfo mai={VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,0,mr.size,0};
      pvkAllocateMemory(dev,&mai,0,&ibm); pvkBindBufferMemory(dev,ib,ibm,0); }

    void *p; pvkMapMemory(dev,vbm,0,sizeof(verts),0,&p); memcpy(p,verts,sizeof(verts)); pvkUnmapMemory(dev,vbm);
    pvkMapMemory(dev,ibm,0,sizeof(indices),0,&p); memcpy(p,indices,sizeof(indices)); pvkUnmapMemory(dev,ibm);

    /* ---- pipeline ---- */
    VkVertexInputBindingDescription vib={0,24,VK_VERTEX_INPUT_RATE_VERTEX};
    VkVertexInputAttributeDescription via[2]={{0,0,VK_FORMAT_R32G32B32_SFLOAT,0},{1,0,VK_FORMAT_R32G32B32_SFLOAT,12}};
    VkPipelineVertexInputStateCreateInfo visci={VK_STRUCTURE_TYPE_PIPELINE_VERTEX_INPUT_STATE_CREATE_INFO,0,0,1,&vib,2,via};
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
    VkSubpassDescription sp={0,VK_PIPELINE_BIND_POINT_GRAPHICS,0,0,1,&ar,0,0,0,0};
    VkRenderPassCreateInfo rpci={VK_STRUCTURE_TYPE_RENDER_PASS_CREATE_INFO,0,0,1,&att,1,&sp,0,0};
    VkRenderPass rp; pvkCreateRenderPass(dev,&rpci,0,&rp);
    VkGraphicsPipelineCreateInfo gpci={VK_STRUCTURE_TYPE_GRAPHICS_PIPELINE_CREATE_INFO,0,0,2,stages,&visci,&iasci,0,&vpsci,&rsci,&msci,0,&cbsci,0,pl,rp,0,0,0};
    VkPipeline pipe; pvkCreateGraphicsPipelines(dev,0,1,&gpci,0,&pipe);

    /* ---- render target ---- */
    VkImageCreateInfo imci={VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO,0,0,VK_IMAGE_TYPE_2D,VK_FORMAT_R8G8B8A8_UNORM,{WIDTH,HEIGHT,1},1,1,VK_SAMPLE_COUNT_1_BIT,VK_IMAGE_TILING_OPTIMAL,VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT|VK_IMAGE_USAGE_TRANSFER_SRC_BIT,VK_SHARING_MODE_EXCLUSIVE,0,0,VK_IMAGE_LAYOUT_UNDEFINED};
    VkImage img; pvkCreateImage(dev,&imci,0,&img);
    VkMemoryRequirements mr; pvkGetImageMemoryRequirements(dev,img,&mr);
    VkMemoryAllocateInfo mai={VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,0,mr.size,0};
    VkDeviceMemory imem; pvkAllocateMemory(dev,&mai,0,&imem);
    pvkBindImageMemory(dev,img,imem,0);
    VkImageViewCreateInfo ivci={VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO,0,0,img,VK_IMAGE_VIEW_TYPE_2D,VK_FORMAT_R8G8B8A8_UNORM,{VK_COMPONENT_SWIZZLE_IDENTITY,VK_COMPONENT_SWIZZLE_IDENTITY,VK_COMPONENT_SWIZZLE_IDENTITY,VK_COMPONENT_SWIZZLE_IDENTITY},{VK_IMAGE_ASPECT_COLOR_BIT,0,1,0,1}};
    VkImageView iv; pvkCreateImageView(dev,&ivci,0,&iv);
    VkFramebufferCreateInfo fci={VK_STRUCTURE_TYPE_FRAMEBUFFER_CREATE_INFO,0,0,rp,1,&iv,WIDTH,HEIGHT,1};
    VkFramebuffer fb; pvkCreateFramebuffer(dev,&fci,0,&fb);

    /* ---- command ---- */
    VkCommandPoolCreateInfo cpi={VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO,0,0,qf};
    VkCommandPool cp; pvkCreateCommandPool(dev,&cpi,0,&cp);
    VkCommandBufferAllocateInfo cbai={VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO,0,cp,VK_COMMAND_BUFFER_LEVEL_PRIMARY,1};
    VkCommandBuffer cb; pvkAllocateCommandBuffers(dev,&cbai,&cb);
    VkFenceCreateInfo fnci={VK_STRUCTURE_TYPE_FENCE_CREATE_INFO,0,0};
    VkFence fence; pvkCreateFence(dev,&fnci,0,&fence);

    LOG("=== Bench %d frames ===", NUM_FRAMES);
    VkDeviceSize off=0;
    double t0 = now_ms();

    for (int frame = 0; frame < NUM_FRAMES; frame++) {
        pvkResetCommandBuffer(cb,0);
        VkCommandBufferBeginInfo cbbi={VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO,0,VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT,0};
        pvkBeginCommandBuffer(cb,&cbbi);
        float a=frame*0.02f;
        float mvp[8]={cosf(a),-sinf(a),0,0, sinf(a),cosf(a),0,0};
        VkClearValue clr={{{0,0,0,1}}};
        VkRenderPassBeginInfo rpbi={VK_STRUCTURE_TYPE_RENDER_PASS_BEGIN_INFO,0,rp,fb,{{0,0},{WIDTH,HEIGHT}},1,&clr};
        pvkCmdBeginRenderPass(cb,&rpbi,VK_SUBPASS_CONTENTS_INLINE);
        pvkCmdSetViewport(cb,0,1,&vp);
        pvkCmdSetScissor(cb,0,1,&sc);
        pvkCmdBindPipeline(cb,VK_PIPELINE_BIND_POINT_GRAPHICS,pipe);
        pvkCmdPushConstants(cb,pl,VK_SHADER_STAGE_VERTEX_BIT,0,32,mvp);
        pvkCmdBindVertexBuffers(cb,0,1,&vb,&off);
        pvkCmdBindIndexBuffer(cb,ib,0,VK_INDEX_TYPE_UINT16);
        for (int d = 0; d < 50; d++) {
            float ox = (d % 10) * 0.12f - 0.54f;
            float oy = (d / 10) * 0.12f - 0.3f;
            float m2[8] = { cosf(a + d * 0.1f), -sinf(a + d * 0.1f), ox, 0,
                            sinf(a + d * 0.1f), cosf(a + d * 0.1f), oy, 0 };
            pvkCmdPushConstants(cb, pl, VK_SHADER_STAGE_VERTEX_BIT, 0, 32, m2);
            pvkCmdDrawIndexed(cb, 36, 1, 0, 0, 0);
        }
        pvkCmdEndRenderPass(cb);
        pvkEndCommandBuffer(cb);
        pvkResetFences(dev,1,&fence);
        VkSubmitInfo si={VK_STRUCTURE_TYPE_SUBMIT_INFO,0,0,0,0,1,&cb,0,0};
        pvkQueueSubmit(q,1,&si,fence);
        pvkWaitForFences(dev,1,&fence,VK_TRUE,UINT64_MAX);
    }

    double t1=now_ms(), elapsed=t1-t0, fps=NUM_FRAMES*1000.0/elapsed;
    LOG("Time: %.1f ms  FPS: %.1f  ms/frame: %.2f", elapsed, fps, elapsed/NUM_FRAMES);

    pvkDestroyFence(dev,fence,0);
    pvkDestroyCommandPool(dev,cp,0);
    pvkDestroyFramebuffer(dev,fb,0); pvkDestroyImageView(dev,iv,0); pvkDestroyImage(dev,img,0); pvkFreeMemory(dev,imem,0);
    pvkDestroyPipeline(dev,pipe,0); pvkDestroyPipelineLayout(dev,pl,0); pvkDestroyRenderPass(dev,rp,0);
    pvkDestroyShaderModule(dev,vs,0); pvkDestroyShaderModule(dev,fs,0);
    pvkFreeMemory(dev,vbm,0); pvkDestroyBuffer(dev,vb,0);
    pvkFreeMemory(dev,ibm,0); pvkDestroyBuffer(dev,ib,0);
    pDestroyDevice(dev,0); pDestroyInstance(inst,0);
    dlclose(icd);
    LOG("done");
    return 0;
}
