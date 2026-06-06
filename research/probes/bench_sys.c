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
#define COUNT_OF(x) (sizeof(x)/sizeof((x)[0]))

static double now_ms(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return ts.tv_sec * 1000.0 + ts.tv_nsec / 1000000.0;
}

#define LOG(fmt,...) do{ fprintf(stderr, fmt "\n", ##__VA_ARGS__); fflush(stderr); }while(0)

int main(void) {
    VkApplicationInfo ai={VK_STRUCTURE_TYPE_APPLICATION_INFO,0,"bench",1,0,VK_API_VERSION_1_2};
    VkInstanceCreateInfo ici={VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO,0,0,&ai,0,0,0,0};
    VkInstance inst=0; vkCreateInstance(&ici,0,&inst);
    if(!inst){LOG("no inst");return 1;}

    uint32_t nd=0; vkEnumeratePhysicalDevices(inst,&nd,0);
    VkPhysicalDevice pd; vkEnumeratePhysicalDevices(inst,&nd,&pd);
    VkPhysicalDeviceProperties pr; vkGetPhysicalDeviceProperties(pd,&pr);
    LOG("GPU: %s", pr.deviceName);

    uint32_t qc=0; vkGetPhysicalDeviceQueueFamilyProperties(pd,&qc,0);
    VkQueueFamilyProperties qp[4]; vkGetPhysicalDeviceQueueFamilyProperties(pd,&qc,qp);
    uint32_t qf=UINT32_MAX;
    for(uint32_t i=0;i<qc&&i<4;i++) if(qp[i].queueFlags&VK_QUEUE_GRAPHICS_BIT){qf=i;break;}
    if(qf==UINT32_MAX)return 1;

    float prio=1.0f;
    VkDeviceQueueCreateInfo qci={VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO,0,0,qf,1,&prio};
    VkDeviceCreateInfo dci={VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO,0,0,1,&qci,0,0,0,0,0};
    VkDevice dev=0; vkCreateDevice(pd,&dci,0,&dev);
    VkQueue q; vkGetDeviceQueue(dev,qf,0,&q);

    /* shaders */
    VkShaderModuleCreateInfo smci={VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO,0,0,vs_spv_size,vs_spv};
    VkShaderModule vs; vkCreateShaderModule(dev,&smci,0,&vs);
    smci.pCode=fs_spv; smci.codeSize=fs_spv_size;
    VkShaderModule fs; vkCreateShaderModule(dev,&smci,0,&fs);

    /* cube geometry */
    float verts[]={
        -0.5f,-0.5f,-0.5f, 1,0,0,   0.5f,-0.5f,-0.5f, 0,1,0,
        -0.5f, 0.5f,-0.5f, 0,0,1,   0.5f, 0.5f,-0.5f, 1,1,0,
        -0.5f,-0.5f, 0.5f, 1,0,1,   0.5f,-0.5f, 0.5f, 0,1,1,
        -0.5f, 0.5f, 0.5f, 1,1,1,   0.5f, 0.5f, 0.5f, 0,0,0,
    };
    uint16_t indices[]={0,1,2,1,3,2,4,5,6,5,7,6,0,4,1,4,5,1,2,3,6,3,7,6,0,2,4,2,6,4,1,5,3,5,7,3};

    VkBuffer vb,ib; VkDeviceMemory vbm,ibm;
    { VkBufferCreateInfo bci={VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO,0,0,sizeof(verts),VK_BUFFER_USAGE_VERTEX_BUFFER_BIT,VK_SHARING_MODE_EXCLUSIVE,0,0};
      vkCreateBuffer(dev,&bci,0,&vb);
      VkMemoryRequirements mr; vkGetBufferMemoryRequirements(dev,vb,&mr);
      VkMemoryAllocateInfo mai={VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,0,mr.size,0};
      vkAllocateMemory(dev,&mai,0,&vbm); vkBindBufferMemory(dev,vb,vbm,0);
      void *p; vkMapMemory(dev,vbm,0,sizeof(verts),0,&p); memcpy(p,verts,sizeof(verts)); vkUnmapMemory(dev,vbm); }
    { VkBufferCreateInfo bci={VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO,0,0,sizeof(indices),VK_BUFFER_USAGE_INDEX_BUFFER_BIT,VK_SHARING_MODE_EXCLUSIVE,0,0};
      vkCreateBuffer(dev,&bci,0,&ib);
      VkMemoryRequirements mr; vkGetBufferMemoryRequirements(dev,ib,&mr);
      VkMemoryAllocateInfo mai={VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,0,mr.size,0};
      vkAllocateMemory(dev,&mai,0,&ibm); vkBindBufferMemory(dev,ib,ibm,0);
      void *p; vkMapMemory(dev,ibm,0,sizeof(indices),0,&p); memcpy(p,indices,sizeof(indices)); vkUnmapMemory(dev,ibm); }

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
    VkPipelineLayout pl; vkCreatePipelineLayout(dev,&plci,0,&pl);
    VkAttachmentDescription att={0,VK_FORMAT_R8G8B8A8_UNORM,VK_SAMPLE_COUNT_1_BIT,VK_ATTACHMENT_LOAD_OP_CLEAR,VK_ATTACHMENT_STORE_OP_STORE,0,0,VK_IMAGE_LAYOUT_UNDEFINED,VK_IMAGE_LAYOUT_GENERAL};
    VkAttachmentReference ar={0,VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL};
    VkSubpassDescription spd={0,VK_PIPELINE_BIND_POINT_GRAPHICS,0,0,1,&ar,0,0,0,0};
    VkRenderPassCreateInfo rpci={VK_STRUCTURE_TYPE_RENDER_PASS_CREATE_INFO,0,0,1,&att,1,&spd,0,0};
    VkRenderPass rp; vkCreateRenderPass(dev,&rpci,0,&rp);
    VkGraphicsPipelineCreateInfo gpci={VK_STRUCTURE_TYPE_GRAPHICS_PIPELINE_CREATE_INFO,0,0,2,stages,&vici,&iasci,0,&vpsci,&rsci,&msci,0,&cbsci,0,pl,rp,0,0,0};
    VkPipeline pipe; vkCreateGraphicsPipelines(dev,0,1,&gpci,0,&pipe);

    VkImageCreateInfo imci={VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO,0,0,VK_IMAGE_TYPE_2D,VK_FORMAT_R8G8B8A8_UNORM,{WIDTH,HEIGHT,1},1,1,VK_SAMPLE_COUNT_1_BIT,VK_IMAGE_TILING_OPTIMAL,VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT|VK_IMAGE_USAGE_TRANSFER_SRC_BIT,VK_SHARING_MODE_EXCLUSIVE,0,0,VK_IMAGE_LAYOUT_UNDEFINED};
    VkImage img; vkCreateImage(dev,&imci,0,&img);
    VkMemoryRequirements mr; vkGetImageMemoryRequirements(dev,img,&mr);
    VkMemoryAllocateInfo mai={VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,0,mr.size,0};
    VkDeviceMemory imem; vkAllocateMemory(dev,&mai,0,&imem);
    vkBindImageMemory(dev,img,imem,0);
    VkImageViewCreateInfo ivci={VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO,0,0,img,VK_IMAGE_VIEW_TYPE_2D,VK_FORMAT_R8G8B8A8_UNORM,{VK_COMPONENT_SWIZZLE_IDENTITY,VK_COMPONENT_SWIZZLE_IDENTITY,VK_COMPONENT_SWIZZLE_IDENTITY,VK_COMPONENT_SWIZZLE_IDENTITY},{VK_IMAGE_ASPECT_COLOR_BIT,0,1,0,1}};
    VkImageView iv; vkCreateImageView(dev,&ivci,0,&iv);
    VkFramebufferCreateInfo fci={VK_STRUCTURE_TYPE_FRAMEBUFFER_CREATE_INFO,0,0,rp,1,&iv,WIDTH,HEIGHT,1};
    VkFramebuffer fb; vkCreateFramebuffer(dev,&fci,0,&fb);

    VkCommandPoolCreateInfo cpi={VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO,0,0,qf};
    VkCommandPool cp; vkCreateCommandPool(dev,&cpi,0,&cp);
    VkCommandBufferAllocateInfo cbai={VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO,0,cp,VK_COMMAND_BUFFER_LEVEL_PRIMARY,1};
    VkCommandBuffer cb; vkAllocateCommandBuffers(dev,&cbai,&cb);
    VkFenceCreateInfo fnci={VK_STRUCTURE_TYPE_FENCE_CREATE_INFO,0,0};
    VkFence fence; vkCreateFence(dev,&fnci,0,&fence);

    LOG("=== System Driver: %d frames ===", NUM_FRAMES);
    VkDeviceSize off=0;
    double t0 = now_ms();

    for (int frame = 0; frame < NUM_FRAMES; frame++) {
        vkResetCommandBuffer(cb,0);
        VkCommandBufferBeginInfo cbbi={VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO,0,VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT,0};
        vkBeginCommandBuffer(cb,&cbbi);
        float a=frame*0.02f;
        float mvp[8]={cosf(a),-sinf(a),0,0, sinf(a),cosf(a),0,0};
        VkClearValue clr={{{0,0,0,1}}};
        VkRenderPassBeginInfo rpbi={VK_STRUCTURE_TYPE_RENDER_PASS_BEGIN_INFO,0,rp,fb,{{0,0},{WIDTH,HEIGHT}},1,&clr};
        vkCmdBeginRenderPass(cb,&rpbi,VK_SUBPASS_CONTENTS_INLINE);
        vkCmdSetViewport(cb,0,1,&vp);
        vkCmdSetScissor(cb,0,1,&sc);
        vkCmdBindPipeline(cb,VK_PIPELINE_BIND_POINT_GRAPHICS,pipe);
        vkCmdPushConstants(cb,pl,VK_SHADER_STAGE_VERTEX_BIT,0,32,mvp);
        vkCmdBindVertexBuffers(cb,0,1,&vb,&off);
        vkCmdBindIndexBuffer(cb,ib,0,VK_INDEX_TYPE_UINT16);
        vkCmdDrawIndexed(cb,36,1,0,0,0);
        vkCmdEndRenderPass(cb);
        vkEndCommandBuffer(cb);
        vkResetFences(dev,1,&fence);
        VkSubmitInfo si={VK_STRUCTURE_TYPE_SUBMIT_INFO,0,0,0,0,1,&cb,0,0};
        vkQueueSubmit(q,1,&si,fence);
        vkWaitForFences(dev,1,&fence,VK_TRUE,UINT64_MAX);
    }

    double t1=now_ms(), elapsed=t1-t0, fps=NUM_FRAMES*1000.0/elapsed;
    LOG("Time: %.1f ms  FPS: %.1f  ms/frame: %.2f", elapsed, fps, elapsed/NUM_FRAMES);

    vkDestroyFence(dev,fence,0); vkDestroyCommandPool(dev,cp,0);
    vkDestroyFramebuffer(dev,fb,0); vkDestroyImageView(dev,iv,0); vkDestroyImage(dev,img,0); vkFreeMemory(dev,imem,0);
    vkDestroyPipeline(dev,pipe,0); vkDestroyPipelineLayout(dev,pl,0); vkDestroyRenderPass(dev,rp,0);
    vkDestroyShaderModule(dev,vs,0); vkDestroyShaderModule(dev,fs,0);
    vkFreeMemory(dev,vbm,0); vkDestroyBuffer(dev,vb,0);
    vkFreeMemory(dev,ibm,0); vkDestroyBuffer(dev,ib,0);
    vkDestroyDevice(dev,0); vkDestroyInstance(inst,0);
    LOG("done");
    return 0;
}
