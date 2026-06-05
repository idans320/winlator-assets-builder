#include <dlfcn.h>
#include <vulkan/vulkan.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "spirv_data.h"

#define LOG(fmt,...) do{ fprintf(stderr, fmt "\n", ##__VA_ARGS__); fflush(stderr); }while(0)

int main(void) {
    setenv("TU_DEBUG", "trace", 1);
    LOG("loading ICD...");
    void *icd = dlopen("/data/local/tmp/libvulkan_freedreno.so", RTLD_NOW);
    if (!icd) { LOG("dlopen fail: %s", dlerror()); return 1; }

    PFN_vkVoidFunction (*gp)(VkInstance,const char*) =
        (PFN_vkVoidFunction(*)(VkInstance,const char*))
        dlsym(icd, "vk_icdGetInstanceProcAddr");
    if (!gp) { LOG("no gp"); return 1; }

    /* ---- instance ---- */
    PFN_vkCreateInstance ci = (PFN_vkCreateInstance)gp(NULL,"vkCreateInstance");
    VkApplicationInfo ai = {VK_STRUCTURE_TYPE_APPLICATION_INFO,0,"tri",1,0,VK_API_VERSION_1_2};
    VkInstanceCreateInfo ici = {VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO,0,0,&ai,0,0,0,0};
    VkInstance inst=0;
    if (ci(&ici,0,&inst)||!inst) { LOG("ci fail"); return 1; }
    LOG("instance OK");

    PFN_vkDestroyInstance di = (PFN_vkDestroyInstance)gp(inst,"vkDestroyInstance");
    PFN_vkEnumeratePhysicalDevices ed = (PFN_vkEnumeratePhysicalDevices)gp(inst,"vkEnumeratePhysicalDevices");
    PFN_vkGetPhysicalDeviceProperties gpp = (PFN_vkGetPhysicalDeviceProperties)gp(inst,"vkGetPhysicalDeviceProperties");
    PFN_vkGetPhysicalDeviceQueueFamilyProperties gqf = (PFN_vkGetPhysicalDeviceQueueFamilyProperties)gp(inst,"vkGetPhysicalDeviceQueueFamilyProperties");
    PFN_vkGetPhysicalDeviceMemoryProperties gmp = (PFN_vkGetPhysicalDeviceMemoryProperties)gp(inst,"vkGetPhysicalDeviceMemoryProperties");
    PFN_vkCreateDevice cd = (PFN_vkCreateDevice)gp(inst,"vkCreateDevice");
    PFN_vkDestroyDevice dd = (PFN_vkDestroyDevice)gp(inst,"vkDestroyDevice");
    PFN_vkGetDeviceQueue gq = (PFN_vkGetDeviceQueue)gp(inst,"vkGetDeviceQueue");

    uint32_t nd=0; ed(inst,&nd,0);
    VkPhysicalDevice pd; ed(inst,&nd,&pd);
    VkPhysicalDeviceProperties pr; gpp(pd,&pr);
    LOG("GPU: %s", pr.deviceName);

    uint32_t qc=0; gqf(pd,&qc,0);
    VkQueueFamilyProperties qp[4]; gqf(pd,&qc,qp);
    uint32_t qf=UINT32_MAX;
    for (uint32_t i=0;i<qc&&i<4;i++) if(qp[i].queueFlags&VK_QUEUE_GRAPHICS_BIT){qf=i;break;}
    LOG("qfam:%u", qf);

    float prio=1.0f;
    VkDeviceQueueCreateInfo qci={VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO,0,0,qf,1,&prio};
    VkDeviceCreateInfo dci={VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO,0,0,1,&qci,0,0,0,0,0};
    VkDevice dev=0;
    if (cd(pd,&dci,0,&dev)||!dev) { LOG("cd fail"); return 1; }
    VkQueue q; gq(dev,qf,0,&q);
    LOG("device OK");

    /* ---- device functions ---- */
    PFN_vkCreateCommandPool ccp = (PFN_vkCreateCommandPool)gp(inst,"vkCreateCommandPool");
    PFN_vkDestroyCommandPool dcp = (PFN_vkDestroyCommandPool)gp(inst,"vkDestroyCommandPool");
    PFN_vkAllocateCommandBuffers acb = (PFN_vkAllocateCommandBuffers)gp(inst,"vkAllocateCommandBuffers");
    PFN_vkBeginCommandBuffer bcb = (PFN_vkBeginCommandBuffer)gp(inst,"vkBeginCommandBuffer");
    PFN_vkEndCommandBuffer ecb = (PFN_vkEndCommandBuffer)gp(inst,"vkEndCommandBuffer");
    PFN_vkQueueSubmit qs = (PFN_vkQueueSubmit)gp(inst,"vkQueueSubmit");
    PFN_vkQueueWaitIdle qwi = (PFN_vkQueueWaitIdle)gp(inst,"vkQueueWaitIdle");
    PFN_vkResetCommandBuffer rcb = (PFN_vkResetCommandBuffer)gp(inst,"vkResetCommandBuffer");

    /* ---- pipeline ---- */
    PFN_vkCreateShaderModule csm = (PFN_vkCreateShaderModule)gp(inst,"vkCreateShaderModule");
    PFN_vkDestroyShaderModule dsm = (PFN_vkDestroyShaderModule)gp(inst,"vkDestroyShaderModule");
    PFN_vkCreatePipelineLayout cpl = (PFN_vkCreatePipelineLayout)gp(inst,"vkCreatePipelineLayout");
    PFN_vkDestroyPipelineLayout dpl = (PFN_vkDestroyPipelineLayout)gp(inst,"vkDestroyPipelineLayout");
    PFN_vkCreateGraphicsPipelines cgp = (PFN_vkCreateGraphicsPipelines)gp(inst,"vkCreateGraphicsPipelines");
    PFN_vkDestroyPipeline dp = (PFN_vkDestroyPipeline)gp(inst,"vkDestroyPipeline");
    PFN_vkCreateRenderPass crp = (PFN_vkCreateRenderPass)gp(inst,"vkCreateRenderPass");
    PFN_vkDestroyRenderPass drp = (PFN_vkDestroyRenderPass)gp(inst,"vkDestroyRenderPass");
    PFN_vkCmdBeginRenderPass cbrp = (PFN_vkCmdBeginRenderPass)gp(inst,"vkCmdBeginRenderPass");
    PFN_vkCmdEndRenderPass cerp = (PFN_vkCmdEndRenderPass)gp(inst,"vkCmdEndRenderPass");
    PFN_vkCmdBindPipeline cbp = (PFN_vkCmdBindPipeline)gp(inst,"vkCmdBindPipeline");
    PFN_vkCmdDraw cdr = (PFN_vkCmdDraw)gp(inst,"vkCmdDraw");
    PFN_vkCmdSetViewport csv = (PFN_vkCmdSetViewport)gp(inst,"vkCmdSetViewport");
    PFN_vkCmdSetScissor css = (PFN_vkCmdSetScissor)gp(inst,"vkCmdSetScissor");

    /* ---- framebuffer ---- */
    PFN_vkCreateImage cimg = (PFN_vkCreateImage)gp(inst,"vkCreateImage");
    PFN_vkDestroyImage dimg = (PFN_vkDestroyImage)gp(inst,"vkDestroyImage");
    PFN_vkGetImageMemoryRequirements gimr = (PFN_vkGetImageMemoryRequirements)gp(inst,"vkGetImageMemoryRequirements");
    PFN_vkAllocateMemory am = (PFN_vkAllocateMemory)gp(inst,"vkAllocateMemory");
    PFN_vkFreeMemory fm = (PFN_vkFreeMemory)gp(inst,"vkFreeMemory");
    PFN_vkBindImageMemory bim = (PFN_vkBindImageMemory)gp(inst,"vkBindImageMemory");
    PFN_vkCreateImageView civ = (PFN_vkCreateImageView)gp(inst,"vkCreateImageView");
    PFN_vkDestroyImageView div = (PFN_vkDestroyImageView)gp(inst,"vkDestroyImageView");
    PFN_vkCreateFramebuffer cfb = (PFN_vkCreateFramebuffer)gp(inst,"vkCreateFramebuffer");
    PFN_vkDestroyFramebuffer dfb = (PFN_vkDestroyFramebuffer)gp(inst,"vkDestroyFramebuffer");

    /* ---- shader modules ---- */
    VkShaderModuleCreateInfo smci = {VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO,0,0,vs_spv_size,vs_spv};
    VkShaderModule vs; csm(dev,&smci,0,&vs);
    smci.pCode=fs_spv; smci.codeSize=fs_spv_size;
    VkShaderModule fs; csm(dev,&smci,0,&fs);

    /* ---- pipeline ---- */
    VkPipelineShaderStageCreateInfo stages[2] = {
        {VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,0,0,VK_SHADER_STAGE_VERTEX_BIT,vs,"main",0},
        {VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,0,0,VK_SHADER_STAGE_FRAGMENT_BIT,fs,"main",0},
    };
    VkPipelineVertexInputStateCreateInfo vi = {VK_STRUCTURE_TYPE_PIPELINE_VERTEX_INPUT_STATE_CREATE_INFO};
    VkPipelineInputAssemblyStateCreateInfo ia = {VK_STRUCTURE_TYPE_PIPELINE_INPUT_ASSEMBLY_STATE_CREATE_INFO,0,0,VK_PRIMITIVE_TOPOLOGY_TRIANGLE_LIST,0};
    VkViewport vp = {0,0,256,256,0,1};
    VkRect2D sc = {{0,0},{256,256}};
    VkPipelineViewportStateCreateInfo vpi = {VK_STRUCTURE_TYPE_PIPELINE_VIEWPORT_STATE_CREATE_INFO,0,0,1,&vp,1,&sc};
    VkPipelineRasterizationStateCreateInfo rs = {
        .sType = VK_STRUCTURE_TYPE_PIPELINE_RASTERIZATION_STATE_CREATE_INFO,
        .polygonMode = VK_POLYGON_MODE_FILL,
        .cullMode = 0,
        .frontFace = VK_FRONT_FACE_CLOCKWISE,
        .lineWidth = 1.0f,
    };
    VkPipelineMultisampleStateCreateInfo ms = {
        .sType = VK_STRUCTURE_TYPE_PIPELINE_MULTISAMPLE_STATE_CREATE_INFO,
        .rasterizationSamples = VK_SAMPLE_COUNT_1_BIT,
    };
    VkPipelineColorBlendAttachmentState cba = {
        .colorWriteMask = VK_COLOR_COMPONENT_R_BIT|VK_COLOR_COMPONENT_G_BIT|VK_COLOR_COMPONENT_B_BIT|VK_COLOR_COMPONENT_A_BIT,
    };
    VkPipelineColorBlendStateCreateInfo cb = {
        .sType = VK_STRUCTURE_TYPE_PIPELINE_COLOR_BLEND_STATE_CREATE_INFO,
        .attachmentCount = 1,
        .pAttachments = &cba,
    };

    VkAttachmentDescription att = {0,VK_FORMAT_R8G8B8A8_UNORM,VK_SAMPLE_COUNT_1_BIT,VK_ATTACHMENT_LOAD_OP_CLEAR,VK_ATTACHMENT_STORE_OP_STORE,VK_ATTACHMENT_LOAD_OP_DONT_CARE,VK_ATTACHMENT_STORE_OP_DONT_CARE,VK_IMAGE_LAYOUT_UNDEFINED,VK_IMAGE_LAYOUT_GENERAL};
    VkAttachmentReference ar = {0,VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL};
    VkSubpassDescription sp = {0,VK_PIPELINE_BIND_POINT_GRAPHICS,0,0,1,&ar,0,0,0,0};
    VkRenderPassCreateInfo rpci = {VK_STRUCTURE_TYPE_RENDER_PASS_CREATE_INFO,0,0,1,&att,1,&sp,0,0};
    VkRenderPass rp; crp(dev,&rpci,0,&rp);

    VkPipelineLayoutCreateInfo plci = {VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO};
    VkPipelineLayout pl; cpl(dev,&plci,0,&pl);

    VkGraphicsPipelineCreateInfo gpci = {VK_STRUCTURE_TYPE_GRAPHICS_PIPELINE_CREATE_INFO,0,0,2,stages,&vi,&ia,0,&vpi,&rs,&ms,0,&cb,0,pl,rp,0,0,0};
    VkPipeline pipe; cgp(dev,0,1,&gpci,0,&pipe);
    LOG("pipeline OK");

    /* ---- render target ---- */
    VkImageCreateInfo imci = {VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO,0,0,VK_IMAGE_TYPE_2D,VK_FORMAT_R8G8B8A8_UNORM,{256,256,1},1,1,VK_SAMPLE_COUNT_1_BIT,VK_IMAGE_TILING_OPTIMAL,VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT|VK_IMAGE_USAGE_TRANSFER_SRC_BIT,VK_SHARING_MODE_EXCLUSIVE,0,0,VK_IMAGE_LAYOUT_UNDEFINED};
    VkImage img; cimg(dev,&imci,0,&img);
    VkMemoryRequirements mr; gimr(dev,img,&mr);
    VkPhysicalDeviceMemoryProperties mp; gmp(pd,&mp);
    uint32_t mt=0; for(;mt<mp.memoryTypeCount;mt++) if(mr.memoryTypeBits&(1u<<mt)) break;
    VkMemoryAllocateInfo mai = {VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,0,mr.size,mt};
    VkDeviceMemory mem; am(dev,&mai,0,&mem);
    bim(dev,img,mem,0);

    VkImageViewCreateInfo ivci = {VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO,0,0,img,VK_IMAGE_VIEW_TYPE_2D,VK_FORMAT_R8G8B8A8_UNORM,{VK_COMPONENT_SWIZZLE_IDENTITY,VK_COMPONENT_SWIZZLE_IDENTITY,VK_COMPONENT_SWIZZLE_IDENTITY,VK_COMPONENT_SWIZZLE_IDENTITY},{VK_IMAGE_ASPECT_COLOR_BIT,0,1,0,1}};
    VkImageView iv; civ(dev,&ivci,0,&iv);
    VkFramebufferCreateInfo fci = {VK_STRUCTURE_TYPE_FRAMEBUFFER_CREATE_INFO,0,0,rp,1,&iv,256,256,1};
    VkFramebuffer fb; cfb(dev,&fci,0,&fb);
    LOG("framebuffer OK");

    /* ---- submit ---- */
    VkCommandPoolCreateInfo cpi = {VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO,0,0,qf};
    VkCommandPool cp; ccp(dev,&cpi,0,&cp);
    VkCommandBufferAllocateInfo cbai = {VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO,0,cp,VK_COMMAND_BUFFER_LEVEL_PRIMARY,1};
    VkCommandBuffer cmdbuf; acb(dev,&cbai,&cmdbuf);

    VkCommandBufferBeginInfo cbbi = {VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO,0,VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT,0};
    bcb(cmdbuf,&cbbi);

    VkClearValue clr = {{{0.0f,0.0f,0.0f,1.0f}}};
    VkRenderPassBeginInfo rpbi = {VK_STRUCTURE_TYPE_RENDER_PASS_BEGIN_INFO,0,rp,fb,{{0,0},{256,256}},1,&clr};
    cbrp(cmdbuf,&rpbi,VK_SUBPASS_CONTENTS_INLINE);
    csv(cmdbuf,0,1,&vp);
    css(cmdbuf,0,1,&sc);
    cbp(cmdbuf,VK_PIPELINE_BIND_POINT_GRAPHICS,pipe);
    LOG("vkCmdDraw...");
    cdr(cmdbuf,3,1,0,0);
    cerp(cmdbuf);
    ecb(cmdbuf);

    LOG("submitting...");
    VkSubmitInfo si = {VK_STRUCTURE_TYPE_SUBMIT_INFO,0,0,0,0,1,&cmdbuf,0,0};
    qs(q,1,&si,0);
    qwi(q);
    LOG("submit done");

    /* ---- teardown ---- */
    rcb(cmdbuf,0);
    dcp(dev,cp,0);
    dfb(dev,fb,0); div(dev,iv,0); dimg(dev,img,0); fm(dev,mem,0);
    dp(dev,pipe,0); dpl(dev,pl,0); drp(dev,rp,0);
    dsm(dev,vs,0); dsm(dev,fs,0);
    dd(dev,0); di(inst,0);
    dlclose(icd);
    LOG("done");
    return 0;
}
