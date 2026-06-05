#ifndef VULKAN_COMMON_H
#define VULKAN_COMMON_H

#include <vulkan/vulkan.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <assert.h>

#define WIDTH  256
#define HEIGHT 256

typedef struct {
    VkInstance       instance;
    VkPhysicalDevice physical_device;
    VkDevice         device;
    VkQueue          queue;
    uint32_t         queue_family;
    VkCommandPool    command_pool;
    VkCommandBuffer  command_buffer;
    VkFence          fence;
} ProbeCtx;

static VkResult create_instance(ProbeCtx *ctx) {
    VkApplicationInfo app = {
        .sType = VK_STRUCTURE_TYPE_APPLICATION_INFO,
        .pApplicationName = "turnip_probe",
        .applicationVersion = VK_MAKE_VERSION(1, 0, 0),
        .pEngineName = "probe",
        .engineVersion = VK_MAKE_VERSION(1, 0, 0),
        .apiVersion = VK_API_VERSION_1_2,
    };

    VkInstanceCreateInfo ci = {
        .sType = VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO,
        .pApplicationInfo = &app,
        .enabledExtensionCount = 0,
        .ppEnabledExtensionNames = NULL,
        .enabledLayerCount = 0,
        .ppEnabledLayerNames = NULL,
    };

    return vkCreateInstance(&ci, NULL, &ctx->instance);
}

static VkResult select_device(ProbeCtx *ctx) {
    uint32_t count = 0;
    vkEnumeratePhysicalDevices(ctx->instance, &count, NULL);
    if (count == 0) return VK_ERROR_INITIALIZATION_FAILED;

    VkPhysicalDevice *devs = malloc(count * sizeof(VkPhysicalDevice));
    vkEnumeratePhysicalDevices(ctx->instance, &count, devs);

    ctx->physical_device = VK_NULL_HANDLE;
    ctx->queue_family = UINT32_MAX;

    for (uint32_t i = 0; i < count; i++) {
        VkPhysicalDeviceProperties props;
        vkGetPhysicalDeviceProperties(devs[i], &props);
        fprintf(stderr, "[PROBE] GPU%d: %s (driver %d.%d.%d)\n",
            i, props.deviceName,
            VK_VERSION_MAJOR(props.driverVersion),
            VK_VERSION_MINOR(props.driverVersion),
            VK_VERSION_PATCH(props.driverVersion));

        if (props.deviceType == VK_PHYSICAL_DEVICE_TYPE_INTEGRATED_GPU ||
            props.deviceType == VK_PHYSICAL_DEVICE_TYPE_DISCRETE_GPU) {

            uint32_t qcount = 0;
            vkGetPhysicalDeviceQueueFamilyProperties(devs[i], &qcount, NULL);
            VkQueueFamilyProperties *qprops = malloc(qcount * sizeof(VkQueueFamilyProperties));
            vkGetPhysicalDeviceQueueFamilyProperties(devs[i], &qcount, qprops);

            for (uint32_t j = 0; j < qcount; j++) {
                if (qprops[j].queueFlags & VK_QUEUE_GRAPHICS_BIT) {
                    ctx->physical_device = devs[i];
                    ctx->queue_family = j;
                    free(qprops);
                    free(devs);
                    return VK_SUCCESS;
                }
            }
            free(qprops);
        }
    }
    free(devs);
    return ctx->physical_device ? VK_SUCCESS : VK_ERROR_INITIALIZATION_FAILED;
}

static VkResult create_logical_device(ProbeCtx *ctx) {
    float priority = 1.0f;
    VkDeviceQueueCreateInfo qci = {
        .sType = VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO,
        .queueFamilyIndex = ctx->queue_family,
        .queueCount = 1,
        .pQueuePriorities = &priority,
    };

    VkDeviceCreateInfo dci = {
        .sType = VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO,
        .queueCreateInfoCount = 1,
        .pQueueCreateInfos = &qci,
    };

    VkResult res = vkCreateDevice(ctx->physical_device, &dci, NULL, &ctx->device);
    if (res != VK_SUCCESS) return res;

    vkGetDeviceQueue(ctx->device, ctx->queue_family, 0, &ctx->queue);
    return VK_SUCCESS;
}

static VkResult create_command_pool(ProbeCtx *ctx) {
    VkCommandPoolCreateInfo cpi = {
        .sType = VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO,
        .queueFamilyIndex = ctx->queue_family,
        .flags = VK_COMMAND_POOL_CREATE_RESET_COMMAND_BUFFER_BIT,
    };
    VkResult res = vkCreateCommandPool(ctx->device, &cpi, NULL, &ctx->command_pool);
    if (res != VK_SUCCESS) return res;

    VkCommandBufferAllocateInfo ai = {
        .sType = VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO,
        .commandPool = ctx->command_pool,
        .level = VK_COMMAND_BUFFER_LEVEL_PRIMARY,
        .commandBufferCount = 1,
    };
    res = vkAllocateCommandBuffers(ctx->device, &ai, &ctx->command_buffer);
    if (res != VK_SUCCESS) return res;

    VkFenceCreateInfo fi = {
        .sType = VK_STRUCTURE_TYPE_FENCE_CREATE_INFO,
        .flags = VK_FENCE_CREATE_SIGNALED_BIT,
    };
    return vkCreateFence(ctx->device, &fi, NULL, &ctx->fence);
}

static VkResult probe_init(ProbeCtx *ctx) {
    VkResult r;
    if ((r = create_instance(ctx)) != VK_SUCCESS)       { fprintf(stderr, "[PROBE] instance: %d\n", r); return r; }
    if ((r = select_device(ctx)) != VK_SUCCESS)          { fprintf(stderr, "[PROBE] device: %d\n", r); return r; }
    if ((r = create_logical_device(ctx)) != VK_SUCCESS)   { fprintf(stderr, "[PROBE] logical: %d\n", r); return r; }
    if ((r = create_command_pool(ctx)) != VK_SUCCESS)     { fprintf(stderr, "[PROBE] cmdpool: %d\n", r); return r; }
    return VK_SUCCESS;
}

static void probe_destroy(ProbeCtx *ctx) {
    vkDestroyFence(ctx->device, ctx->fence, NULL);
    vkDestroyCommandPool(ctx->device, ctx->command_pool, NULL);
    vkDestroyDevice(ctx->device, NULL);
    vkDestroyInstance(ctx->instance, NULL);
}

static VkResult submit_and_wait(ProbeCtx *ctx) {
    VkSubmitInfo si = {
        .sType = VK_STRUCTURE_TYPE_SUBMIT_INFO,
        .commandBufferCount = 1,
        .pCommandBuffers = &ctx->command_buffer,
    };
    vkResetFences(ctx->device, 1, &ctx->fence);
    VkResult r = vkQueueSubmit(ctx->queue, 1, &si, ctx->fence);
    if (r != VK_SUCCESS) return r;
    return vkWaitForFences(ctx->device, 1, &ctx->fence, VK_TRUE, UINT64_MAX);
}

static VkResult begin_cmds(ProbeCtx *ctx) {
    VkCommandBufferBeginInfo bi = {
        .sType = VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO,
        .flags = VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT,
    };
    return vkBeginCommandBuffer(ctx->command_buffer, &bi);
}

static VkResult end_cmds(ProbeCtx *ctx) {
    return vkEndCommandBuffer(ctx->command_buffer);
}

#endif
