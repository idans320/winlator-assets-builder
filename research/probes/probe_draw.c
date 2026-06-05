#include "vulkan_common.h"
#include "spirv_data.h"

int main(int argc, char **argv) {
    (void)argc; (void)argv;
    ProbeCtx ctx = {0};

    VkResult r = probe_init(&ctx);
    if (r != VK_SUCCESS) {
        fprintf(stderr, "[probe_draw] init failed: %d\n", r);
        return 1;
    }
    fprintf(stderr, "[probe_draw] init OK\n");

    /* Graphics pipeline with SPIR-V shaders */
    VkShaderModuleCreateInfo smci = {
        .sType = VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO,
        .codeSize = vs_spv_size,
        .pCode = vs_spv,
    };
    VkShaderModule vs_mod;
    vkCreateShaderModule(ctx.device, &smci, NULL, &vs_mod);

    smci.codeSize = fs_spv_size;
    smci.pCode = fs_spv;
    VkShaderModule fs_mod;
    vkCreateShaderModule(ctx.device, &smci, NULL, &fs_mod);

    VkPipelineShaderStageCreateInfo stages[2] = {
        { .sType = VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,
          .stage = VK_SHADER_STAGE_VERTEX_BIT, .module = vs_mod, .pName = "main" },
        { .sType = VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,
          .stage = VK_SHADER_STAGE_FRAGMENT_BIT, .module = fs_mod, .pName = "main" },
    };

    VkPipelineVertexInputStateCreateInfo vi = {
        .sType = VK_STRUCTURE_TYPE_PIPELINE_VERTEX_INPUT_STATE_CREATE_INFO,
    };
    VkPipelineInputAssemblyStateCreateInfo ia = {
        .sType = VK_STRUCTURE_TYPE_PIPELINE_INPUT_ASSEMBLY_STATE_CREATE_INFO,
        .topology = VK_PRIMITIVE_TOPOLOGY_TRIANGLE_LIST,
    };
    VkViewport vp = { 0, 0, (float)WIDTH, (float)HEIGHT, 0.0f, 1.0f };
    VkRect2D scissor = { {0, 0}, {WIDTH, HEIGHT} };
    VkPipelineViewportStateCreateInfo vpci = {
        .sType = VK_STRUCTURE_TYPE_PIPELINE_VIEWPORT_STATE_CREATE_INFO,
        .viewportCount = 1, .pViewports = &vp,
        .scissorCount = 1, .pScissors = &scissor,
    };
    VkPipelineRasterizationStateCreateInfo rs = {
        .sType = VK_STRUCTURE_TYPE_PIPELINE_RASTERIZATION_STATE_CREATE_INFO,
        .lineWidth = 1.0f,
    };
    VkPipelineMultisampleStateCreateInfo ms = {
        .sType = VK_STRUCTURE_TYPE_PIPELINE_MULTISAMPLE_STATE_CREATE_INFO,
        .rasterizationSamples = VK_SAMPLE_COUNT_1_BIT,
    };
    VkPipelineColorBlendAttachmentState cba = {
        .colorWriteMask = VK_COLOR_COMPONENT_R_BIT | VK_COLOR_COMPONENT_G_BIT |
                          VK_COLOR_COMPONENT_B_BIT | VK_COLOR_COMPONENT_A_BIT,
    };
    VkPipelineColorBlendStateCreateInfo cb = {
        .sType = VK_STRUCTURE_TYPE_PIPELINE_COLOR_BLEND_STATE_CREATE_INFO,
        .attachmentCount = 1, .pAttachments = &cba,
    };

    VkAttachmentDescription att = {
        .format = VK_FORMAT_R8G8B8A8_UNORM,
        .samples = VK_SAMPLE_COUNT_1_BIT,
        .loadOp = VK_ATTACHMENT_LOAD_OP_CLEAR,
        .storeOp = VK_ATTACHMENT_STORE_OP_STORE,
        .initialLayout = VK_IMAGE_LAYOUT_UNDEFINED,
        .finalLayout = VK_IMAGE_LAYOUT_GENERAL,
    };
    VkAttachmentReference att_ref = { 0, VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL };

    VkSubpassDescription subpass = {
        .pipelineBindPoint = VK_PIPELINE_BIND_POINT_GRAPHICS,
        .colorAttachmentCount = 1, .pColorAttachments = &att_ref,
    };
    VkRenderPassCreateInfo rpci = {
        .sType = VK_STRUCTURE_TYPE_RENDER_PASS_CREATE_INFO,
        .attachmentCount = 1, .pAttachments = &att,
        .subpassCount = 1, .pSubpasses = &subpass,
    };
    VkRenderPass render_pass;
    vkCreateRenderPass(ctx.device, &rpci, NULL, &render_pass);

    VkPipelineLayoutCreateInfo plci = {
        .sType = VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO,
    };
    VkPipelineLayout pipeline_layout;
    vkCreatePipelineLayout(ctx.device, &plci, NULL, &pipeline_layout);

    VkGraphicsPipelineCreateInfo gpci = {
        .sType = VK_STRUCTURE_TYPE_GRAPHICS_PIPELINE_CREATE_INFO,
        .stageCount = 2, .pStages = stages,
        .pVertexInputState = &vi,
        .pInputAssemblyState = &ia,
        .pViewportState = &vpci,
        .pRasterizationState = &rs,
        .pMultisampleState = &ms,
        .pColorBlendState = &cb,
        .layout = pipeline_layout,
        .renderPass = render_pass,
    };
    VkPipeline pipeline;
    vkCreateGraphicsPipelines(ctx.device, VK_NULL_HANDLE, 1, &gpci, NULL, &pipeline);

    fprintf(stderr, "[probe_draw] pipeline created. Submitting draw...\n");

    VkImageCreateInfo ici = {
        .sType = VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO,
        .imageType = VK_IMAGE_TYPE_2D,
        .format = VK_FORMAT_R8G8B8A8_UNORM,
        .extent = {WIDTH, HEIGHT, 1},
        .mipLevels = 1, .arrayLayers = 1,
        .samples = VK_SAMPLE_COUNT_1_BIT,
        .tiling = VK_IMAGE_TILING_OPTIMAL,
        .usage = VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT | VK_IMAGE_USAGE_TRANSFER_SRC_BIT,
        .initialLayout = VK_IMAGE_LAYOUT_UNDEFINED,
    };
    VkImage image;
    vkCreateImage(ctx.device, &ici, NULL, &image);

    VkMemoryRequirements mem_req;
    vkGetImageMemoryRequirements(ctx.device, image, &mem_req);

    VkPhysicalDeviceMemoryProperties mem_props;
    vkGetPhysicalDeviceMemoryProperties(ctx.physical_device, &mem_props);

    uint32_t mem_type = 0;
    for (; mem_type < mem_props.memoryTypeCount; mem_type++) {
        if (mem_req.memoryTypeBits & (1u << mem_type)) break;
    }

    VkMemoryAllocateInfo mai = {
        .sType = VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,
        .allocationSize = mem_req.size,
        .memoryTypeIndex = mem_type,
    };
    VkDeviceMemory image_mem;
    vkAllocateMemory(ctx.device, &mai, NULL, &image_mem);
    vkBindImageMemory(ctx.device, image, image_mem, 0);

    VkImageViewCreateInfo ivci = {
        .sType = VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO,
        .image = image,
        .viewType = VK_IMAGE_VIEW_TYPE_2D,
        .format = VK_FORMAT_R8G8B8A8_UNORM,
        .subresourceRange = {VK_IMAGE_ASPECT_COLOR_BIT, 0, 1, 0, 1},
    };
    VkImageView image_view;
    vkCreateImageView(ctx.device, &ivci, NULL, &image_view);

    VkFramebufferCreateInfo fci = {
        .sType = VK_STRUCTURE_TYPE_FRAMEBUFFER_CREATE_INFO,
        .renderPass = render_pass,
        .attachmentCount = 1, .pAttachments = &image_view,
        .width = WIDTH, .height = HEIGHT, .layers = 1,
    };
    VkFramebuffer framebuffer;
    vkCreateFramebuffer(ctx.device, &fci, NULL, &framebuffer);

    begin_cmds(&ctx);

    VkClearValue clear = {{{ 0.0f, 0.0f, 0.0f, 1.0f }}};
    VkRenderPassBeginInfo rpbi = {
        .sType = VK_STRUCTURE_TYPE_RENDER_PASS_BEGIN_INFO,
        .renderPass = render_pass,
        .framebuffer = framebuffer,
        .renderArea = {{0, 0}, {WIDTH, HEIGHT}},
        .clearValueCount = 1, .pClearValues = &clear,
    };
    vkCmdBeginRenderPass(ctx.command_buffer, &rpbi, VK_SUBPASS_CONTENTS_INLINE);

    vkCmdSetViewport(ctx.command_buffer, 0, 1, &vp);
    vkCmdSetScissor(ctx.command_buffer, 0, 1, &scissor);
    vkCmdBindPipeline(ctx.command_buffer, VK_PIPELINE_BIND_POINT_GRAPHICS, pipeline);
    vkCmdDraw(ctx.command_buffer, 3, 1, 0, 0);

    vkCmdEndRenderPass(ctx.command_buffer);
    end_cmds(&ctx);

    fprintf(stderr, "[probe_draw] submitting...\n");
    r = submit_and_wait(&ctx);
    fprintf(stderr, "[probe_draw] result: %d\n", r);

    vkDestroyFramebuffer(ctx.device, framebuffer, NULL);
    vkDestroyImageView(ctx.device, image_view, NULL);
    vkDestroyImage(ctx.device, image, NULL);
    vkFreeMemory(ctx.device, image_mem, NULL);
    vkDestroyPipeline(ctx.device, pipeline, NULL);
    vkDestroyPipelineLayout(ctx.device, pipeline_layout, NULL);
    vkDestroyRenderPass(ctx.device, render_pass, NULL);
    vkDestroyShaderModule(ctx.device, vs_mod, NULL);
    vkDestroyShaderModule(ctx.device, fs_mod, NULL);

    probe_destroy(&ctx);
    fprintf(stderr, "[probe_draw] done\n");
    return r == VK_SUCCESS ? 0 : 1;
}
