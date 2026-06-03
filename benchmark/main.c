#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <math.h>
#include <time.h>
#include <unistd.h>
#include <vulkan/vulkan.h>

#include "shaders.h"

/* ================================================================
 * Configuration
 * ================================================================ */

#define COMPUTE_WORKGROUP_COUNT 4096
#define GRAPHICS_TRIANGLE_COUNT 50000
#define GRAPHICS_RENDER_WIDTH   1024
#define GRAPHICS_RENDER_HEIGHT  1024
#define MAX_TIMESTAMP_SAMPLES   512
#define WARMUP_ITERATIONS       3
#define BENCH_ITERATIONS        20

/* ================================================================
 * Utility
 * ================================================================ */

static const char* vk_error_str(VkResult r) {
    switch (r) {
        case VK_SUCCESS: return "VK_SUCCESS";
        case VK_NOT_READY: return "VK_NOT_READY";
        case VK_TIMEOUT: return "VK_TIMEOUT";
        case VK_EVENT_SET: return "VK_EVENT_SET";
        case VK_EVENT_RESET: return "VK_EVENT_RESET";
        case VK_INCOMPLETE: return "VK_INCOMPLETE";
        case VK_ERROR_OUT_OF_HOST_MEMORY: return "OUT_OF_HOST_MEMORY";
        case VK_ERROR_OUT_OF_DEVICE_MEMORY: return "OUT_OF_DEVICE_MEMORY";
        case VK_ERROR_INITIALIZATION_FAILED: return "INITIALIZATION_FAILED";
        case VK_ERROR_DEVICE_LOST: return "DEVICE_LOST";
        case VK_ERROR_MEMORY_MAP_FAILED: return "MEMORY_MAP_FAILED";
        case VK_ERROR_LAYER_NOT_PRESENT: return "LAYER_NOT_PRESENT";
        case VK_ERROR_EXTENSION_NOT_PRESENT: return "EXTENSION_NOT_PRESENT";
        case VK_ERROR_FEATURE_NOT_PRESENT: return "FEATURE_NOT_PRESENT";
        case VK_ERROR_INCOMPATIBLE_DRIVER: return "INCOMPATIBLE_DRIVER";
        default: return "UNKNOWN";
    }
}

#define CHECK_VK(call, msg) do { \
    VkResult _r = (call); \
    if (_r != VK_SUCCESS) { \
        fprintf(stderr, "%s: %s (%d)\n", msg, vk_error_str(_r), _r); \
        return 1; \
    } \
} while(0)

#define CHECK_VK_NULL(ptr, msg) do { \
    if (!(ptr)) { \
        fprintf(stderr, "%s: null\n", msg); \
        return 1; \
    } \
} while(0)

static uint32_t find_memory_type(VkPhysicalDeviceMemoryProperties* memprops,
                                  uint32_t type_filter, VkMemoryPropertyFlags flags) {
    for (uint32_t i = 0; i < memprops->memoryTypeCount; i++) {
        if ((type_filter & (1u << i)) &&
            (memprops->memoryTypes[i].propertyFlags & flags) == flags) {
            return i;
        }
    }
    return UINT32_MAX;
}

static double get_time_us(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (double)ts.tv_sec * 1e6 + (double)ts.tv_nsec / 1e3;
}

typedef struct {
    double values[MAX_TIMESTAMP_SAMPLES];
    int count;
} BenchmarkStats;

static void stats_push(BenchmarkStats* s, double v) {
    if (s->count < MAX_TIMESTAMP_SAMPLES)
        s->values[s->count++] = v;
}

static int cmp_double(const void* a, const void* b) {
    double da = *(const double*)a, db = *(const double*)b;
    return (da > db) - (da < db);
}

static double stats_median(BenchmarkStats* s) {
    if (s->count == 0) return 0;
    double sorted[MAX_TIMESTAMP_SAMPLES];
    memcpy(sorted, s->values, sizeof(double) * s->count);
    qsort(sorted, s->count, sizeof(double), cmp_double);
    if (s->count % 2 == 0)
        return (sorted[s->count/2 - 1] + sorted[s->count/2]) / 2.0;
    return sorted[s->count/2];
}

static double stats_min(BenchmarkStats* s) {
    double m = s->values[0];
    for (int i = 1; i < s->count; i++)
        if (s->values[i] < m) m = s->values[i];
    return m;
}

static double stats_max(BenchmarkStats* s) {
    double m = s->values[0];
    for (int i = 1; i < s->count; i++)
        if (s->values[i] > m) m = s->values[i];
    return m;
}

static double stats_avg(BenchmarkStats* s) {
    double sum = 0;
    for (int i = 0; i < s->count; i++) sum += s->values[i];
    return s->count > 0 ? sum / s->count : 0;
}

static double stats_stddev(BenchmarkStats* s) {
    double avg = stats_avg(s);
    double sum = 0;
    for (int i = 0; i < s->count; i++) {
        double d = s->values[i] - avg;
        sum += d * d;
    }
    return s->count > 1 ? sqrt(sum / (s->count - 1)) : 0;
}

/* ================================================================
 * Vulkan State
 * ================================================================ */

typedef struct {
    VkInstance instance;
    VkPhysicalDevice physical_device;
    VkPhysicalDeviceProperties device_props;
    VkPhysicalDeviceMemoryProperties mem_props;
    VkDevice device;
    VkQueue queue;
    uint32_t queue_family;
    VkCommandPool cmd_pool;
    VkDescriptorPool desc_pool;
    VkQueryPool timestamp_pool;
    float timestamp_period;
    int timestamp_bits;
    /* FP16 capabilities */
    int has_shader_float16;
    int has_16bit_storage;
    int has_storage_buffer_16bit;
    int has_uniform_buffer_16bit;
    int has_float16_int8_ext;
} VkState;

static VkState g_vk;

static int vk_init(void) {
    /* Instance */
    VkApplicationInfo app_info = {
        .sType = VK_STRUCTURE_TYPE_APPLICATION_INFO,
        .pApplicationName = "bench-vulkan",
        .applicationVersion = VK_MAKE_VERSION(1, 0, 0),
        .pEngineName = "bench-vulkan",
        .engineVersion = VK_MAKE_VERSION(1, 0, 0),
        .apiVersion = VK_API_VERSION_1_0,
    };

    const char* inst_exts[] = { };
    const char* inst_layers[] = { };

    VkInstanceCreateInfo inst_ci = {
        .sType = VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO,
        .pApplicationInfo = &app_info,
        .enabledExtensionCount = 0,
        .ppEnabledExtensionNames = inst_exts,
        .enabledLayerCount = 0,
        .ppEnabledLayerNames = inst_layers,
    };

    CHECK_VK(vkCreateInstance(&inst_ci, NULL, &g_vk.instance), "vkCreateInstance");

    /* Physical device */
    uint32_t dev_count = 0;
    vkEnumeratePhysicalDevices(g_vk.instance, &dev_count, NULL);
    if (dev_count == 0) {
        fprintf(stderr, "No Vulkan-capable devices found\n");
        return 1;
    }
    VkPhysicalDevice* devices = malloc(sizeof(VkPhysicalDevice) * dev_count);
    vkEnumeratePhysicalDevices(g_vk.instance, &dev_count, devices);
    g_vk.physical_device = devices[0];
    free(devices);

    vkGetPhysicalDeviceProperties(g_vk.physical_device, &g_vk.device_props);
    vkGetPhysicalDeviceMemoryProperties(g_vk.physical_device, &g_vk.mem_props);

    /* Probe FP16 capabilities */
    g_vk.has_shader_float16 = 0;
    g_vk.has_16bit_storage = 0;
    g_vk.has_float16_int8_ext = 0;

    uint32_t ext_count = 0;
    vkEnumerateDeviceExtensionProperties(g_vk.physical_device, NULL, &ext_count, NULL);
    VkExtensionProperties* exts = malloc(sizeof(VkExtensionProperties) * ext_count);
    vkEnumerateDeviceExtensionProperties(g_vk.physical_device, NULL, &ext_count, exts);
    for (uint32_t e = 0; e < ext_count; e++) {
        if (!strcmp(exts[e].extensionName, VK_KHR_SHADER_FLOAT16_INT8_EXTENSION_NAME))
            g_vk.has_float16_int8_ext = 1;
        if (!strcmp(exts[e].extensionName, VK_KHR_16BIT_STORAGE_EXTENSION_NAME))
            g_vk.has_16bit_storage = 1;
    }
    free(exts);

    if (g_vk.has_float16_int8_ext) {
        VkPhysicalDeviceShaderFloat16Int8Features f16_features = {
            .sType = VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_SHADER_FLOAT16_INT8_FEATURES,
        };
        VkPhysicalDeviceFeatures2 feats2 = {
            .sType = VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_FEATURES_2,
            .pNext = &f16_features,
        };
        vkGetPhysicalDeviceFeatures2(g_vk.physical_device, &feats2);
        g_vk.has_shader_float16 = f16_features.shaderFloat16 ? 1 : 0;
    }

    if (g_vk.has_16bit_storage) {
        VkPhysicalDevice16BitStorageFeatures s16_features = {
            .sType = VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_16BIT_STORAGE_FEATURES,
        };
        VkPhysicalDeviceFeatures2 feats2 = {
            .sType = VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_FEATURES_2,
            .pNext = &s16_features,
        };
        vkGetPhysicalDeviceFeatures2(g_vk.physical_device, &feats2);
        g_vk.has_storage_buffer_16bit = s16_features.storageBuffer16BitAccess ? 1 : 0;
        g_vk.has_uniform_buffer_16bit = s16_features.uniformAndStorageBuffer16BitAccess ? 1 : 0;
    }

    /* Queue family */
    uint32_t qf_count = 0;
    vkGetPhysicalDeviceQueueFamilyProperties(g_vk.physical_device, &qf_count, NULL);
    VkQueueFamilyProperties* qf_props = malloc(sizeof(VkQueueFamilyProperties) * qf_count);
    vkGetPhysicalDeviceQueueFamilyProperties(g_vk.physical_device, &qf_count, qf_props);

    g_vk.queue_family = UINT32_MAX;
    for (uint32_t i = 0; i < qf_count; i++) {
        if (qf_props[i].queueFlags & (VK_QUEUE_GRAPHICS_BIT | VK_QUEUE_COMPUTE_BIT)) {
            g_vk.queue_family = i;
            break;
        }
    }
    free(qf_props);

    if (g_vk.queue_family == UINT32_MAX) {
        fprintf(stderr, "No suitable queue family found\n");
        return 1;
    }

    /* Device */
    float q_prio = 1.0f;
    VkDeviceQueueCreateInfo q_ci = {
        .sType = VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO,
        .queueFamilyIndex = g_vk.queue_family,
        .queueCount = 1,
        .pQueuePriorities = &q_prio,
    };

    int dev_ext_count = 0;
    const char* dev_exts[4];
    (void)dev_exts; /* suppress unused warning if no extensions */
    if (g_vk.has_float16_int8_ext)
        dev_exts[dev_ext_count++] = VK_KHR_SHADER_FLOAT16_INT8_EXTENSION_NAME;
    if (g_vk.has_16bit_storage)
        dev_exts[dev_ext_count++] = VK_KHR_16BIT_STORAGE_EXTENSION_NAME;

    VkPhysicalDeviceShaderFloat16Int8Features f16_feats = {
        .sType = VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_SHADER_FLOAT16_INT8_FEATURES,
        .shaderFloat16 = VK_TRUE,
    };
    VkPhysicalDevice16BitStorageFeatures s16_feats = {
        .sType = VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_16BIT_STORAGE_FEATURES,
        .storageBuffer16BitAccess = g_vk.has_storage_buffer_16bit ? VK_TRUE : VK_FALSE,
        .uniformAndStorageBuffer16BitAccess = g_vk.has_uniform_buffer_16bit ? VK_TRUE : VK_FALSE,
    };

    VkPhysicalDeviceFeatures features = { 0 };
    VkDeviceCreateInfo dev_ci = {
        .sType = VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO,
        .queueCreateInfoCount = 1,
        .pQueueCreateInfos = &q_ci,
        .enabledExtensionCount = dev_ext_count,
        .ppEnabledExtensionNames = dev_ext_count ? dev_exts : NULL,
        .pEnabledFeatures = &features,
    };

    /* pNext chain: float16_int8 -> 16bit_storage */
    void** pnext = (void**)&dev_ci.pNext;
    if (g_vk.has_float16_int8_ext) {
        f16_feats.shaderFloat16 = g_vk.has_shader_float16 ? VK_TRUE : VK_FALSE;
        *pnext = &f16_feats;
        pnext = &f16_feats.pNext;
    }
    if (g_vk.has_16bit_storage) {
        *pnext = &s16_feats;
        pnext = &s16_feats.pNext;
    }

    CHECK_VK(vkCreateDevice(g_vk.physical_device, &dev_ci, NULL, &g_vk.device),
             "vkCreateDevice");
    vkGetDeviceQueue(g_vk.device, g_vk.queue_family, 0, &g_vk.queue);

    /* Command pool */
    VkCommandPoolCreateInfo cp_ci = {
        .sType = VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO,
        .queueFamilyIndex = g_vk.queue_family,
        .flags = VK_COMMAND_POOL_CREATE_RESET_COMMAND_BUFFER_BIT,
    };
    CHECK_VK(vkCreateCommandPool(g_vk.device, &cp_ci, NULL, &g_vk.cmd_pool),
             "vkCreateCommandPool");

    /* Descriptor pool */
    VkDescriptorPoolSize dp_sizes[] = {
        { VK_DESCRIPTOR_TYPE_STORAGE_BUFFER, 4 },
        { VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER, 4 },
    };
    VkDescriptorPoolCreateInfo dp_ci = {
        .sType = VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO,
        .maxSets = 8,
        .poolSizeCount = 2,
        .pPoolSizes = dp_sizes,
    };
    CHECK_VK(vkCreateDescriptorPool(g_vk.device, &dp_ci, NULL, &g_vk.desc_pool),
             "vkCreateDescriptorPool");

    /* Timestamp query pool */
    g_vk.timestamp_bits = g_vk.device_props.limits.timestampComputeAndGraphics ? 1 : 0;
    if (!g_vk.timestamp_bits) {
        fprintf(stderr, "Warning: device does not support timestamp queries\n");
    }
    g_vk.timestamp_period = g_vk.device_props.limits.timestampPeriod;

    VkQueryPoolCreateInfo qp_ci = {
        .sType = VK_STRUCTURE_TYPE_QUERY_POOL_CREATE_INFO,
        .queryType = VK_QUERY_TYPE_TIMESTAMP,
        .queryCount = MAX_TIMESTAMP_SAMPLES,
    };
    CHECK_VK(vkCreateQueryPool(g_vk.device, &qp_ci, NULL, &g_vk.timestamp_pool),
             "vkCreateQueryPool");

    return 0;
}

static void vk_cleanup(void) {
    if (g_vk.timestamp_pool) vkDestroyQueryPool(g_vk.device, g_vk.timestamp_pool, NULL);
    if (g_vk.desc_pool) vkDestroyDescriptorPool(g_vk.device, g_vk.desc_pool, NULL);
    if (g_vk.cmd_pool) vkDestroyCommandPool(g_vk.device, g_vk.cmd_pool, NULL);
    if (g_vk.device) vkDestroyDevice(g_vk.device, NULL);
    if (g_vk.instance) vkDestroyInstance(g_vk.instance, NULL);
}

static int create_buffer(VkDeviceSize size, VkBufferUsageFlags usage,
                          VkMemoryPropertyFlags mem_flags,
                          VkBuffer* buf, VkDeviceMemory* mem) {
    VkBufferCreateInfo buf_ci = {
        .sType = VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO,
        .size = size,
        .usage = usage,
        .sharingMode = VK_SHARING_MODE_EXCLUSIVE,
    };
    CHECK_VK(vkCreateBuffer(g_vk.device, &buf_ci, NULL, buf), "vkCreateBuffer");

    VkMemoryRequirements mem_req;
    vkGetBufferMemoryRequirements(g_vk.device, *buf, &mem_req);

    uint32_t mem_type = find_memory_type(&g_vk.mem_props, mem_req.memoryTypeBits, mem_flags);
    if (mem_type == UINT32_MAX) {
        fprintf(stderr, "No suitable memory type for buffer\n");
        return 1;
    }

    VkMemoryAllocateInfo alloc_info = {
        .sType = VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,
        .allocationSize = mem_req.size,
        .memoryTypeIndex = mem_type,
    };
    CHECK_VK(vkAllocateMemory(g_vk.device, &alloc_info, NULL, mem), "vkAllocateMemory");
    CHECK_VK(vkBindBufferMemory(g_vk.device, *buf, *mem, 0), "vkBindBufferMemory");
    return 0;
}

static int create_image(VkFormat format, uint32_t width, uint32_t height,
                         VkImageUsageFlags usage,
                         VkImage* img, VkDeviceMemory* mem) {
    VkImageCreateInfo img_ci = {
        .sType = VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO,
        .imageType = VK_IMAGE_TYPE_2D,
        .format = format,
        .extent = { width, height, 1 },
        .mipLevels = 1,
        .arrayLayers = 1,
        .samples = VK_SAMPLE_COUNT_1_BIT,
        .tiling = VK_IMAGE_TILING_OPTIMAL,
        .usage = usage,
        .sharingMode = VK_SHARING_MODE_EXCLUSIVE,
        .initialLayout = VK_IMAGE_LAYOUT_UNDEFINED,
    };
    CHECK_VK(vkCreateImage(g_vk.device, &img_ci, NULL, img), "vkCreateImage");

    VkMemoryRequirements mem_req;
    vkGetImageMemoryRequirements(g_vk.device, *img, &mem_req);

    uint32_t mem_type = find_memory_type(&g_vk.mem_props, mem_req.memoryTypeBits,
                                          VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT);
    if (mem_type == UINT32_MAX) {
        fprintf(stderr, "No suitable memory type for image\n");
        return 1;
    }

    VkMemoryAllocateInfo alloc_info = {
        .sType = VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,
        .allocationSize = mem_req.size,
        .memoryTypeIndex = mem_type,
    };
    CHECK_VK(vkAllocateMemory(g_vk.device, &alloc_info, NULL, mem), "vkAllocateMemory");
    CHECK_VK(vkBindImageMemory(g_vk.device, *img, *mem, 0), "vkBindImageMemory");
    return 0;
}

static VkCommandBuffer begin_cmd(void) {
    VkCommandBufferAllocateInfo alloc_info = {
        .sType = VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO,
        .commandPool = g_vk.cmd_pool,
        .level = VK_COMMAND_BUFFER_LEVEL_PRIMARY,
        .commandBufferCount = 1,
    };
    VkCommandBuffer cmd;
    if (vkAllocateCommandBuffers(g_vk.device, &alloc_info, &cmd) != VK_SUCCESS)
        return VK_NULL_HANDLE;

    VkCommandBufferBeginInfo begin_info = {
        .sType = VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO,
        .flags = VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT,
    };
    vkBeginCommandBuffer(cmd, &begin_info);
    return cmd;
}

static void end_submit_wait(VkCommandBuffer cmd) {
    vkEndCommandBuffer(cmd);

    VkSubmitInfo submit_info = {
        .sType = VK_STRUCTURE_TYPE_SUBMIT_INFO,
        .commandBufferCount = 1,
        .pCommandBuffers = &cmd,
    };
    vkQueueSubmit(g_vk.queue, 1, &submit_info, VK_NULL_HANDLE);
    vkQueueWaitIdle(g_vk.queue);
    vkFreeCommandBuffers(g_vk.device, g_vk.cmd_pool, 1, &cmd);
}

/* ================================================================
 * Compute Benchmark
 * ================================================================ */

static int bench_compute(int iterations, BenchmarkStats* gpu_times,
                          BenchmarkStats* cpu_times) {
    VkShaderModuleCreateInfo sm_ci = {
        .sType = VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO,
        .codeSize = compute_spv_len,
        .pCode = (const uint32_t*)compute_spv_data,
    };
    VkShaderModule comp_module;
    CHECK_VK(vkCreateShaderModule(g_vk.device, &sm_ci, NULL, &comp_module),
             "vkCreateShaderModule(compute)");

    VkPipelineShaderStageCreateInfo stage_ci = {
        .sType = VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,
        .stage = VK_SHADER_STAGE_COMPUTE_BIT,
        .module = comp_module,
        .pName = "main",
    };

    VkPipelineLayoutCreateInfo pl_ci = {
        .sType = VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO,
        .setLayoutCount = 0,
    };
    VkPipelineLayout pipeline_layout;
    CHECK_VK(vkCreatePipelineLayout(g_vk.device, &pl_ci, NULL, &pipeline_layout),
             "vkCreatePipelineLayout(compute)");

    VkComputePipelineCreateInfo cp_ci = {
        .sType = VK_STRUCTURE_TYPE_COMPUTE_PIPELINE_CREATE_INFO,
        .stage = stage_ci,
        .layout = pipeline_layout,
    };
    VkPipeline pipeline;
    CHECK_VK(vkCreateComputePipelines(g_vk.device, VK_NULL_HANDLE, 1, &cp_ci,
                                       NULL, &pipeline), "vkCreateComputePipeline");

    for (int i = 0; i < iterations; i++) {
        int q_idx = i * 2;

        VkCommandBuffer cmd = begin_cmd();
        if (!cmd) return 1;

        vkCmdResetQueryPool(cmd, g_vk.timestamp_pool, q_idx, 2);

        double cpu_start = get_time_us();

        vkCmdWriteTimestamp(cmd, VK_PIPELINE_STAGE_COMPUTE_SHADER_BIT,
                            g_vk.timestamp_pool, q_idx);
        vkCmdBindPipeline(cmd, VK_PIPELINE_BIND_POINT_COMPUTE, pipeline);
        vkCmdDispatch(cmd, COMPUTE_WORKGROUP_COUNT, 1, 1);
        vkCmdWriteTimestamp(cmd, VK_PIPELINE_STAGE_COMPUTE_SHADER_BIT,
                            g_vk.timestamp_pool, q_idx + 1);

        vkEndCommandBuffer(cmd);

        VkSubmitInfo submit_info = {
            .sType = VK_STRUCTURE_TYPE_SUBMIT_INFO,
            .commandBufferCount = 1,
            .pCommandBuffers = &cmd,
        };
        vkQueueSubmit(g_vk.queue, 1, &submit_info, VK_NULL_HANDLE);
        vkQueueWaitIdle(g_vk.queue);

        double cpu_end = get_time_us();
        stats_push(cpu_times, cpu_end - cpu_start);

        uint64_t timestamps[2];
        vkGetQueryPoolResults(g_vk.device, g_vk.timestamp_pool, q_idx, 2,
                              sizeof(timestamps), timestamps, sizeof(uint64_t),
                              VK_QUERY_RESULT_64_BIT | VK_QUERY_RESULT_WAIT_BIT);
        double gpu_time = (timestamps[1] - timestamps[0]) * g_vk.timestamp_period / 1000.0;
        stats_push(gpu_times, gpu_time);

        vkFreeCommandBuffers(g_vk.device, g_vk.cmd_pool, 1, &cmd);
    }

    vkDestroyPipeline(g_vk.device, pipeline, NULL);
    vkDestroyPipelineLayout(g_vk.device, pipeline_layout, NULL);
    vkDestroyShaderModule(g_vk.device, comp_module, NULL);

    return 0;
}

/* ================================================================
 * FP16 Compute Benchmark (FP16 vs FP32 throughput)
 * ================================================================ */

static int bench_fp16_compute(int iterations, BenchmarkStats* fp16_gpu_times,
                               BenchmarkStats* fp16_cpu_times,
                               BenchmarkStats* fp32_gpu_times,
                               BenchmarkStats* fp32_cpu_times,
                               float* fp16_speedup) {
    if (!g_vk.has_shader_float16) return 1;

    /* Build FP16 pipeline */
    VkShaderModuleCreateInfo fp16_sm_ci = {
        .sType = VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO,
        .codeSize = fp16_compute_spv_len,
        .pCode = (const uint32_t*)fp16_compute_spv_data,
    };
    VkShaderModule fp16_mod;
    CHECK_VK(vkCreateShaderModule(g_vk.device, &fp16_sm_ci, NULL, &fp16_mod),
             "vkCreateShaderModule(fp16)");

    VkPipelineShaderStageCreateInfo fp16_stage = {
        .sType = VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,
        .stage = VK_SHADER_STAGE_COMPUTE_BIT,
        .module = fp16_mod,
        .pName = "main",
    };

    VkPipelineLayoutCreateInfo pl_ci = {
        .sType = VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO,
        .setLayoutCount = 0,
    };
    VkPipelineLayout fp16_layout;
    CHECK_VK(vkCreatePipelineLayout(g_vk.device, &pl_ci, NULL, &fp16_layout),
             "vkCreatePipelineLayout(fp16)");

    VkComputePipelineCreateInfo fp16_cp_ci = {
        .sType = VK_STRUCTURE_TYPE_COMPUTE_PIPELINE_CREATE_INFO,
        .stage = fp16_stage,
        .layout = fp16_layout,
    };
    VkPipeline fp16_pipe;
    CHECK_VK(vkCreateComputePipelines(g_vk.device, VK_NULL_HANDLE, 1, &fp16_cp_ci,
                                       NULL, &fp16_pipe), "vkCreateComputePipeline(fp16)");

    /* Build FP32 pipeline (same as bench_compute, but local) */
    VkShaderModuleCreateInfo fp32_sm_ci = {
        .sType = VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO,
        .codeSize = compute_spv_len,
        .pCode = (const uint32_t*)compute_spv_data,
    };
    VkShaderModule fp32_mod;
    CHECK_VK(vkCreateShaderModule(g_vk.device, &fp32_sm_ci, NULL, &fp32_mod),
             "vkCreateShaderModule(fp32)");

    VkPipelineShaderStageCreateInfo fp32_stage = {
        .sType = VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,
        .stage = VK_SHADER_STAGE_COMPUTE_BIT,
        .module = fp32_mod,
        .pName = "main",
    };

    VkPipelineLayout fp32_layout;
    CHECK_VK(vkCreatePipelineLayout(g_vk.device, &pl_ci, NULL, &fp32_layout),
             "vkCreatePipelineLayout(fp32)");

    VkComputePipelineCreateInfo fp32_cp_ci = {
        .sType = VK_STRUCTURE_TYPE_COMPUTE_PIPELINE_CREATE_INFO,
        .stage = fp32_stage,
        .layout = fp32_layout,
    };
    VkPipeline fp32_pipe;
    CHECK_VK(vkCreateComputePipelines(g_vk.device, VK_NULL_HANDLE, 1, &fp32_cp_ci,
                                       NULL, &fp32_pipe), "vkCreateComputePipeline(fp32)");

    /* Run FP16 benchmark */
    for (int i = 0; i < iterations; i++) {
        int q_idx = i * 4;

        VkCommandBuffer cmd = begin_cmd();
        if (!cmd) return 1;

        vkCmdResetQueryPool(cmd, g_vk.timestamp_pool, q_idx, 2);

        double cpu_start = get_time_us();
        vkCmdWriteTimestamp(cmd, VK_PIPELINE_STAGE_COMPUTE_SHADER_BIT,
                            g_vk.timestamp_pool, q_idx);
        vkCmdBindPipeline(cmd, VK_PIPELINE_BIND_POINT_COMPUTE, fp16_pipe);
        vkCmdDispatch(cmd, COMPUTE_WORKGROUP_COUNT, 1, 1);
        vkCmdWriteTimestamp(cmd, VK_PIPELINE_STAGE_COMPUTE_SHADER_BIT,
                            g_vk.timestamp_pool, q_idx + 1);

        vkEndCommandBuffer(cmd);
        VkSubmitInfo submit_info = {
            .sType = VK_STRUCTURE_TYPE_SUBMIT_INFO,
            .commandBufferCount = 1, .pCommandBuffers = &cmd,
        };
        vkQueueSubmit(g_vk.queue, 1, &submit_info, VK_NULL_HANDLE);
        vkQueueWaitIdle(g_vk.queue);
        double cpu_end = get_time_us();
        stats_push(fp16_cpu_times, cpu_end - cpu_start);

        uint64_t ts[2];
        vkGetQueryPoolResults(g_vk.device, g_vk.timestamp_pool, q_idx, 2,
                              sizeof(ts), ts, sizeof(uint64_t),
                              VK_QUERY_RESULT_64_BIT | VK_QUERY_RESULT_WAIT_BIT);
        stats_push(fp16_gpu_times, (ts[1] - ts[0]) * g_vk.timestamp_period / 1000.0);
        vkFreeCommandBuffers(g_vk.device, g_vk.cmd_pool, 1, &cmd);
    }

    /* Run FP32 benchmark (interleaved for fair comparison) */
    for (int i = 0; i < iterations; i++) {
        int q_idx = i * 4 + 2;

        VkCommandBuffer cmd = begin_cmd();
        if (!cmd) return 1;

        vkCmdResetQueryPool(cmd, g_vk.timestamp_pool, q_idx, 2);

        double cpu_start = get_time_us();
        vkCmdWriteTimestamp(cmd, VK_PIPELINE_STAGE_COMPUTE_SHADER_BIT,
                            g_vk.timestamp_pool, q_idx);
        vkCmdBindPipeline(cmd, VK_PIPELINE_BIND_POINT_COMPUTE, fp32_pipe);
        vkCmdDispatch(cmd, COMPUTE_WORKGROUP_COUNT, 1, 1);
        vkCmdWriteTimestamp(cmd, VK_PIPELINE_STAGE_COMPUTE_SHADER_BIT,
                            g_vk.timestamp_pool, q_idx + 1);

        vkEndCommandBuffer(cmd);
        VkSubmitInfo submit_info = {
            .sType = VK_STRUCTURE_TYPE_SUBMIT_INFO,
            .commandBufferCount = 1, .pCommandBuffers = &cmd,
        };
        vkQueueSubmit(g_vk.queue, 1, &submit_info, VK_NULL_HANDLE);
        vkQueueWaitIdle(g_vk.queue);
        double cpu_end = get_time_us();
        stats_push(fp32_cpu_times, cpu_end - cpu_start);

        uint64_t ts[2];
        vkGetQueryPoolResults(g_vk.device, g_vk.timestamp_pool, q_idx, 2,
                              sizeof(ts), ts, sizeof(uint64_t),
                              VK_QUERY_RESULT_64_BIT | VK_QUERY_RESULT_WAIT_BIT);
        stats_push(fp32_gpu_times, (ts[1] - ts[0]) * g_vk.timestamp_period / 1000.0);
        vkFreeCommandBuffers(g_vk.device, g_vk.cmd_pool, 1, &cmd);
    }

    *fp16_speedup = stats_avg(fp32_gpu_times) > 0
        ? (float)(stats_avg(fp32_gpu_times) / stats_avg(fp16_gpu_times)) : 0.0f;

    vkDestroyPipeline(g_vk.device, fp16_pipe, NULL);
    vkDestroyPipelineLayout(g_vk.device, fp16_layout, NULL);
    vkDestroyShaderModule(g_vk.device, fp16_mod, NULL);
    vkDestroyPipeline(g_vk.device, fp32_pipe, NULL);
    vkDestroyPipelineLayout(g_vk.device, fp32_layout, NULL);
    vkDestroyShaderModule(g_vk.device, fp32_mod, NULL);

    return 0;
}

/* ================================================================
 * Graphics Benchmark (offscreen)
 * ================================================================ */

static int bench_graphics(int iterations, BenchmarkStats* gpu_times,
                           BenchmarkStats* cpu_times) {
    /* Shaders */
    VkShaderModuleCreateInfo vs_ci = {
        .sType = VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO,
        .codeSize = vert_spv_len,
        .pCode = (const uint32_t*)vert_spv_data,
    };
    VkShaderModule vs;
    CHECK_VK(vkCreateShaderModule(g_vk.device, &vs_ci, NULL, &vs),
             "vkCreateShaderModule(vert)");

    VkShaderModuleCreateInfo fs_ci = {
        .sType = VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO,
        .codeSize = frag_spv_len,
        .pCode = (const uint32_t*)frag_spv_data,
    };
    VkShaderModule fs;
    CHECK_VK(vkCreateShaderModule(g_vk.device, &fs_ci, NULL, &fs),
             "vkCreateShaderModule(frag)");

    /* Render target */
    VkImage rt_img; VkDeviceMemory rt_mem;
    if (create_image(VK_FORMAT_R8G8B8A8_UNORM,
                      GRAPHICS_RENDER_WIDTH, GRAPHICS_RENDER_HEIGHT,
                      VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT | VK_IMAGE_USAGE_TRANSFER_SRC_BIT,
                      &rt_img, &rt_mem) != 0) return 1;

    VkImageViewCreateInfo iv_ci = {
        .sType = VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO,
        .image = rt_img,
        .viewType = VK_IMAGE_VIEW_TYPE_2D,
        .format = VK_FORMAT_R8G8B8A8_UNORM,
        .subresourceRange = { VK_IMAGE_ASPECT_COLOR_BIT, 0, 1, 0, 1 },
    };
    VkImageView rt_view;
    CHECK_VK(vkCreateImageView(g_vk.device, &iv_ci, NULL, &rt_view),
             "vkCreateImageView(rt)");

    /* Render pass */
    VkAttachmentDescription att = {
        .format = VK_FORMAT_R8G8B8A8_UNORM,
        .samples = VK_SAMPLE_COUNT_1_BIT,
        .loadOp = VK_ATTACHMENT_LOAD_OP_CLEAR,
        .storeOp = VK_ATTACHMENT_STORE_OP_STORE,
        .initialLayout = VK_IMAGE_LAYOUT_UNDEFINED,
        .finalLayout = VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,
    };
    VkAttachmentReference att_ref = { 0, VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL };
    VkSubpassDescription subpass = {
        .pipelineBindPoint = VK_PIPELINE_BIND_POINT_GRAPHICS,
        .colorAttachmentCount = 1,
        .pColorAttachments = &att_ref,
    };
    VkRenderPassCreateInfo rp_ci = {
        .sType = VK_STRUCTURE_TYPE_RENDER_PASS_CREATE_INFO,
        .attachmentCount = 1,
        .pAttachments = &att,
        .subpassCount = 1,
        .pSubpasses = &subpass,
    };
    VkRenderPass render_pass;
    CHECK_VK(vkCreateRenderPass(g_vk.device, &rp_ci, NULL, &render_pass),
             "vkCreateRenderPass");

    /* Framebuffer */
    VkFramebufferCreateInfo fb_ci = {
        .sType = VK_STRUCTURE_TYPE_FRAMEBUFFER_CREATE_INFO,
        .renderPass = render_pass,
        .attachmentCount = 1,
        .pAttachments = &rt_view,
        .width = GRAPHICS_RENDER_WIDTH,
        .height = GRAPHICS_RENDER_HEIGHT,
        .layers = 1,
    };
    VkFramebuffer framebuffer;
    CHECK_VK(vkCreateFramebuffer(g_vk.device, &fb_ci, NULL, &framebuffer),
             "vkCreateFramebuffer");

    /* Pipeline */
    VkPipelineShaderStageCreateInfo stages[] = {
        { .sType = VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,
          .stage = VK_SHADER_STAGE_VERTEX_BIT, .module = vs, .pName = "main" },
        { .sType = VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,
          .stage = VK_SHADER_STAGE_FRAGMENT_BIT, .module = fs, .pName = "main" },
    };

    VkVertexInputBindingDescription vib = {
        .binding = 0, .stride = 5 * sizeof(float), .inputRate = VK_VERTEX_INPUT_RATE_VERTEX,
    };
    VkVertexInputAttributeDescription via[] = {
        { 0, 0, VK_FORMAT_R32G32B32_SFLOAT, 0 },
        { 1, 0, VK_FORMAT_R32G32_SFLOAT, 3 * sizeof(float) },
    };
    VkPipelineVertexInputStateCreateInfo vi_ci = {
        .sType = VK_STRUCTURE_TYPE_PIPELINE_VERTEX_INPUT_STATE_CREATE_INFO,
        .vertexBindingDescriptionCount = 1,
        .pVertexBindingDescriptions = &vib,
        .vertexAttributeDescriptionCount = 2,
        .pVertexAttributeDescriptions = via,
    };
    VkPipelineInputAssemblyStateCreateInfo ia_ci = {
        .sType = VK_STRUCTURE_TYPE_PIPELINE_INPUT_ASSEMBLY_STATE_CREATE_INFO,
        .topology = VK_PRIMITIVE_TOPOLOGY_TRIANGLE_LIST,
    };
    VkPipelineViewportStateCreateInfo vp_ci = {
        .sType = VK_STRUCTURE_TYPE_PIPELINE_VIEWPORT_STATE_CREATE_INFO,
        .viewportCount = 1, .scissorCount = 1,
    };
    VkPipelineRasterizationStateCreateInfo rs_ci = {
        .sType = VK_STRUCTURE_TYPE_PIPELINE_RASTERIZATION_STATE_CREATE_INFO,
        .polygonMode = VK_POLYGON_MODE_FILL,
        .cullMode = VK_CULL_MODE_NONE,
        .frontFace = VK_FRONT_FACE_COUNTER_CLOCKWISE,
        .lineWidth = 1.0f,
    };
    VkPipelineMultisampleStateCreateInfo ms_ci = {
        .sType = VK_STRUCTURE_TYPE_PIPELINE_MULTISAMPLE_STATE_CREATE_INFO,
        .rasterizationSamples = VK_SAMPLE_COUNT_1_BIT,
    };
    VkPipelineColorBlendAttachmentState cba = {
        .colorWriteMask = VK_COLOR_COMPONENT_R_BIT | VK_COLOR_COMPONENT_G_BIT |
                          VK_COLOR_COMPONENT_B_BIT | VK_COLOR_COMPONENT_A_BIT,
    };
    VkPipelineColorBlendStateCreateInfo cb_ci = {
        .sType = VK_STRUCTURE_TYPE_PIPELINE_COLOR_BLEND_STATE_CREATE_INFO,
        .attachmentCount = 1,
        .pAttachments = &cba,
    };
    VkDynamicState dyn_states[] = { VK_DYNAMIC_STATE_VIEWPORT, VK_DYNAMIC_STATE_SCISSOR };
    VkPipelineDynamicStateCreateInfo dyn_ci = {
        .sType = VK_STRUCTURE_TYPE_PIPELINE_DYNAMIC_STATE_CREATE_INFO,
        .dynamicStateCount = 2,
        .pDynamicStates = dyn_states,
    };

    VkPipelineLayoutCreateInfo pl_ci = {
        .sType = VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO,
        .setLayoutCount = 0,
    };
    VkPipelineLayout pipeline_layout;
    CHECK_VK(vkCreatePipelineLayout(g_vk.device, &pl_ci, NULL, &pipeline_layout),
             "vkCreatePipelineLayout(graphics)");

    VkGraphicsPipelineCreateInfo gp_ci = {
        .sType = VK_STRUCTURE_TYPE_GRAPHICS_PIPELINE_CREATE_INFO,
        .stageCount = 2, .pStages = stages,
        .pVertexInputState = &vi_ci,
        .pInputAssemblyState = &ia_ci,
        .pViewportState = &vp_ci,
        .pRasterizationState = &rs_ci,
        .pMultisampleState = &ms_ci,
        .pColorBlendState = &cb_ci,
        .pDynamicState = &dyn_ci,
        .layout = pipeline_layout,
        .renderPass = render_pass,
        .subpass = 0,
    };
    VkPipeline pipeline;
    CHECK_VK(vkCreateGraphicsPipelines(g_vk.device, VK_NULL_HANDLE, 1, &gp_ci,
                                        NULL, &pipeline), "vkCreateGraphicsPipeline");

    /* Vertex buffer */
    int tri_count = GRAPHICS_TRIANGLE_COUNT;
    int vert_count = tri_count * 3;
    VkDeviceSize vb_size = sizeof(float) * 5 * vert_count;
    float* vert_data = malloc(vb_size);
    for (int t = 0; t < tri_count; t++) {
        float x = (float)(t % 1000) / 1000.0f * 2.0f - 1.0f;
        float y = (float)(t / 1000) / 50.0f * 2.0f - 1.0f;
        float* v = vert_data + t * 15;
        v[0] = x - 0.001f; v[1] = y - 0.01f;  v[2] = 0.0f; v[3] = 0.0f; v[4] = 0.0f;
        v[5] = x + 0.001f; v[6] = y - 0.01f;  v[7] = 0.0f; v[8] = 1.0f; v[9] = 0.0f;
        v[10]= x;          v[11]= y + 0.01f;  v[12]= 0.0f; v[13]=0.5f; v[14]=1.0f;
    }

    VkBuffer vb; VkDeviceMemory vb_mem;
    VkBuffer staging; VkDeviceMemory staging_mem;
    if (create_buffer(vb_size, VK_BUFFER_USAGE_TRANSFER_DST_BIT | VK_BUFFER_USAGE_VERTEX_BUFFER_BIT,
                       VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT, &vb, &vb_mem) != 0) return 1;
    if (create_buffer(vb_size, VK_BUFFER_USAGE_TRANSFER_SRC_BIT,
                       VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT,
                       &staging, &staging_mem) != 0) return 1;

    void* mapped;
    vkMapMemory(g_vk.device, staging_mem, 0, vb_size, 0, &mapped);
    memcpy(mapped, vert_data, vb_size);
    vkUnmapMemory(g_vk.device, staging_mem);
    free(vert_data);

    VkCommandBuffer setup = begin_cmd();
    VkBufferCopy copy_region = { 0, 0, vb_size };
    vkCmdCopyBuffer(setup, staging, vb, 1, &copy_region);
    end_submit_wait(setup);

    for (int i = 0; i < iterations; i++) {
        int q_idx = i * 2;

        VkCommandBuffer cmd = begin_cmd();
        if (!cmd) return 1;

        vkCmdResetQueryPool(cmd, g_vk.timestamp_pool, q_idx, 2);

        VkClearValue clear = {{{ 0.0f, 0.0f, 0.0f, 0.0f }}};
        VkRenderPassBeginInfo rp_begin = {
            .sType = VK_STRUCTURE_TYPE_RENDER_PASS_BEGIN_INFO,
            .renderPass = render_pass,
            .framebuffer = framebuffer,
            .renderArea = {{ 0, 0 }, { GRAPHICS_RENDER_WIDTH, GRAPHICS_RENDER_HEIGHT }},
            .clearValueCount = 1,
            .pClearValues = &clear,
        };

        double cpu_start = get_time_us();

        vkCmdWriteTimestamp(cmd, VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT,
                            g_vk.timestamp_pool, q_idx);

        VkViewport viewport = { 0, 0, GRAPHICS_RENDER_WIDTH, GRAPHICS_RENDER_HEIGHT, 0, 1 };
        VkRect2D scissor = {{ 0, 0 }, { GRAPHICS_RENDER_WIDTH, GRAPHICS_RENDER_HEIGHT }};
        vkCmdSetViewport(cmd, 0, 1, &viewport);
        vkCmdSetScissor(cmd, 0, 1, &scissor);

        vkCmdBeginRenderPass(cmd, &rp_begin, VK_SUBPASS_CONTENTS_INLINE);
        vkCmdBindPipeline(cmd, VK_PIPELINE_BIND_POINT_GRAPHICS, pipeline);

        VkDeviceSize offset = 0;
        vkCmdBindVertexBuffers(cmd, 0, 1, &vb, &offset);
        vkCmdDraw(cmd, vert_count, 1, 0, 0);

        vkCmdEndRenderPass(cmd);
        vkCmdWriteTimestamp(cmd, VK_PIPELINE_STAGE_BOTTOM_OF_PIPE_BIT,
                            g_vk.timestamp_pool, q_idx + 1);

        vkEndCommandBuffer(cmd);

        VkSubmitInfo submit_info = {
            .sType = VK_STRUCTURE_TYPE_SUBMIT_INFO,
            .commandBufferCount = 1,
            .pCommandBuffers = &cmd,
        };
        vkQueueSubmit(g_vk.queue, 1, &submit_info, VK_NULL_HANDLE);
        vkQueueWaitIdle(g_vk.queue);

        double cpu_end = get_time_us();
        stats_push(cpu_times, cpu_end - cpu_start);

        uint64_t timestamps[2];
        vkGetQueryPoolResults(g_vk.device, g_vk.timestamp_pool, q_idx, 2,
                              sizeof(timestamps), timestamps, sizeof(uint64_t),
                              VK_QUERY_RESULT_64_BIT | VK_QUERY_RESULT_WAIT_BIT);
        double gpu_time = (timestamps[1] - timestamps[0]) * g_vk.timestamp_period / 1000.0;
        stats_push(gpu_times, gpu_time);

        vkFreeCommandBuffers(g_vk.device, g_vk.cmd_pool, 1, &cmd);
    }

    vkDestroyPipeline(g_vk.device, pipeline, NULL);
    vkDestroyPipelineLayout(g_vk.device, pipeline_layout, NULL);
    vkDestroyFramebuffer(g_vk.device, framebuffer, NULL);
    vkDestroyRenderPass(g_vk.device, render_pass, NULL);
    vkDestroyImageView(g_vk.device, rt_view, NULL);
    vkDestroyImage(g_vk.device, rt_img, NULL);
    vkFreeMemory(g_vk.device, rt_mem, NULL);
    vkDestroyShaderModule(g_vk.device, vs, NULL);
    vkDestroyShaderModule(g_vk.device, fs, NULL);
    vkDestroyBuffer(g_vk.device, vb, NULL);
    vkFreeMemory(g_vk.device, vb_mem, NULL);
    vkDestroyBuffer(g_vk.device, staging, NULL);
    vkFreeMemory(g_vk.device, staging_mem, NULL);

    return 0;
}

/* ================================================================
 * JSON Output
 * ================================================================ */

static void json_device_info(FILE* f) {
    VkPhysicalDeviceProperties* p = &g_vk.device_props;
    fprintf(f,
        "  \"device\": {\n"
        "    \"name\": \"%s\",\n"
        "    \"api_version\": \"%d.%d.%d\",\n"
        "    \"driver_version\": \"%d.%d.%d\",\n"
        "    \"vendor_id\": \"0x%04x\",\n"
        "    \"device_id\": \"0x%04x\",\n"
        "    \"device_type\": %d,\n"
        "    \"timestamp_period_ns\": %.2f,\n"
        "    \"max_compute_workgroup_invocations\": %u,\n"
        "    \"max_compute_workgroup_count\": [%u, %u, %u],\n"
        "    \"fp16_capabilities\": {\n"
        "      \"shader_float16\": %s,\n"
        "      \"storage_buffer_16bit\": %s,\n"
        "      \"uniform_buffer_16bit\": %s,\n"
        "      \"float16_int8_extension\": %s\n"
        "    }\n"
        "  },\n",
        p->deviceName,
        VK_API_VERSION_MAJOR(p->apiVersion),
        VK_API_VERSION_MINOR(p->apiVersion),
        VK_API_VERSION_PATCH(p->apiVersion),
        VK_API_VERSION_MAJOR(p->driverVersion),
        VK_API_VERSION_MINOR(p->driverVersion),
        VK_API_VERSION_PATCH(p->driverVersion),
        p->vendorID, p->deviceID,
        p->deviceType,
        g_vk.timestamp_period,
        p->limits.maxComputeWorkGroupInvocations,
        p->limits.maxComputeWorkGroupCount[0],
        p->limits.maxComputeWorkGroupCount[1],
        p->limits.maxComputeWorkGroupCount[2],
        g_vk.has_shader_float16 ? "true" : "false",
        g_vk.has_storage_buffer_16bit ? "true" : "false",
        g_vk.has_uniform_buffer_16bit ? "true" : "false",
        g_vk.has_float16_int8_ext ? "true" : "false");
}

static void json_benchmark(FILE* f, const char* name, const char* workload_type,
                            BenchmarkStats* gpu, BenchmarkStats* cpu,
                            int iterations, const char* extra) {
    fprintf(f,
        "    {\n"
        "      \"name\": \"%s\",\n"
        "      \"type\": \"%s\",\n"
        "      \"iterations\": %d,\n"
        "      \"gpu_time_us\": {\n"
        "        \"min\": %.2f,\n"
        "        \"max\": %.2f,\n"
        "        \"avg\": %.2f,\n"
        "        \"median\": %.2f,\n"
        "        \"stddev\": %.2f\n"
        "      },\n"
        "      \"cpu_time_us\": {\n"
        "        \"min\": %.2f,\n"
        "        \"max\": %.2f,\n"
        "        \"avg\": %.2f,\n"
        "        \"median\": %.2f,\n"
        "        \"stddev\": %.2f\n"
        "      }",
        name, workload_type, iterations,
        stats_min(gpu), stats_max(gpu), stats_avg(gpu), stats_median(gpu), stats_stddev(gpu),
        stats_min(cpu), stats_max(cpu), stats_avg(cpu), stats_median(cpu), stats_stddev(cpu));

    if (extra) fprintf(f, ",\n%s", extra);
    fprintf(f, "\n    }");
}

/* ================================================================
 * ICD Setup (arbitrary .so loading)
 * ================================================================ */

static char g_icd_path[256] = {0};
static const char* g_driver_so_path = NULL;

static int setup_icd_for_driver(const char* driver_so) {
    const char* tmpdir = getenv("TMPDIR");
    if (!tmpdir) tmpdir = "/tmp";

    snprintf(g_icd_path, sizeof(g_icd_path), "%s/bench_icd_%d.json", tmpdir, getpid());

    FILE* f = fopen(g_icd_path, "w");
    if (!f) {
        fprintf(stderr, "Failed to create ICD JSON at %s\n", g_icd_path);
        return 1;
    }
    fprintf(f,
        "{\n"
        "  \"file_format_version\": \"1.0.0\",\n"
        "  \"ICD\": {\n"
        "    \"library_path\": \"%s\",\n"
        "    \"api_version\": \"1.3.0\"\n"
        "  }\n"
        "}\n", driver_so);
    fclose(f);

    setenv("VK_ICD_FILENAMES", g_icd_path, 1);
    g_driver_so_path = driver_so;

    fprintf(stderr, "ICD: %s -> %s\n", g_icd_path, driver_so);
    return 0;
}

static void cleanup_icd(void) {
    if (g_icd_path[0]) {
        unlink(g_icd_path);
        g_icd_path[0] = '\0';
    }
}

static void print_usage(const char* prog) {
    fprintf(stderr,
        "Usage: %s [options]\n"
        "Options:\n"
        "  --driver-so PATH  Load specified Turnip .so (creates temp ICD JSON)\n"
        "  --iterations N    Benchmark iterations (default: %d)\n"
        "  --compute         Run compute benchmark only\n"
        "  --graphics        Run graphics benchmark only\n"
        "  --fp16-bench      Run FP16 vs FP32 compute throughput comparison\n"
        "  --compute-size N  Compute workgroup count (default: %d)\n"
        "  --draw-count N    Graphics triangle count (default: %d)\n"
        "  --output FILE     Write JSON output to file (default: stdout)\n"
        "  --compact         Compact JSON output (single line)\n"
        "  --help            Show this help\n",
        prog, BENCH_ITERATIONS, COMPUTE_WORKGROUP_COUNT, GRAPHICS_TRIANGLE_COUNT);
}

int main(int argc, char** argv) {
    int iterations = BENCH_ITERATIONS;
    int run_compute = 1, run_graphics = 1, run_fp16 = 0;
    int compute_size = COMPUTE_WORKGROUP_COUNT;
    int draw_count = GRAPHICS_TRIANGLE_COUNT;
    const char* output_file = NULL;
    const char* driver_so = NULL;
    int compact = 0;

    for (int i = 1; i < argc; i++) {
        if (!strcmp(argv[i], "--help")) {
            print_usage(argv[0]);
            return 0;
        } else if (!strcmp(argv[i], "--compute")) {
            run_graphics = 0;
        } else if (!strcmp(argv[i], "--graphics")) {
            run_compute = 0;
        } else if (!strcmp(argv[i], "--fp16-bench")) {
            run_fp16 = 1;
        } else if (!strcmp(argv[i], "--compact")) {
            compact = 1;
        } else if (!strcmp(argv[i], "--driver-so") && i + 1 < argc) {
            driver_so = argv[++i];
        } else if (!strcmp(argv[i], "--iterations") && i + 1 < argc) {
            iterations = atoi(argv[++i]);
        } else if (!strcmp(argv[i], "--compute-size") && i + 1 < argc) {
            compute_size = atoi(argv[++i]);
        } else if (!strcmp(argv[i], "--draw-count") && i + 1 < argc) {
            draw_count = atoi(argv[++i]);
        } else if (!strcmp(argv[i], "--output") && i + 1 < argc) {
            output_file = argv[++i];
        } else {
            print_usage(argv[0]);
            return 1;
        }
    }

    if (driver_so) {
        if (access(driver_so, R_OK) != 0) {
            fprintf(stderr, "ERROR: driver not found or not readable: %s\n", driver_so);
            return 1;
        }
        if (setup_icd_for_driver(driver_so) != 0) return 1;
    }

    int queries_per_iter = (run_compute ? 2 : 0) + (run_graphics ? 2 : 0) + (run_fp16 ? 4 : 0);
    if (iterations < 1 || iterations * queries_per_iter > MAX_TIMESTAMP_SAMPLES) {
        fprintf(stderr, "Iterations must be between 1 and %d for current workload selection\n",
                MAX_TIMESTAMP_SAMPLES / queries_per_iter);
        return 1;
    }

    if (vk_init() != 0) {
        fprintf(stderr, "Vulkan initialization failed\n");
        return 1;
    }

    if (!g_vk.timestamp_bits) {
        fprintf(stderr, "ERROR: device does not support timestamp queries\n");
        fprintf(stderr, "  timestampComputeAndGraphics is VK_FALSE on this GPU/driver\n");
        vk_cleanup();
        return 1;
    }

    FILE* out = output_file ? fopen(output_file, "w") : stdout;
    if (!out) { perror(output_file); return 1; }

    const char* nl = compact ? "" : "\n";
    const char* sp = compact ? " " : "  ";

    BenchmarkStats comp_gpu = {0}, comp_cpu = {0};
    BenchmarkStats gfx_gpu = {0}, gfx_cpu = {0};
    BenchmarkStats fp16_gpu = {0}, fp16_cpu = {0};
    BenchmarkStats fp32_gpu = {0}, fp32_cpu = {0};
    float fp16_speedup = 0.0f;
    int comp_ok = 0, gfx_ok = 0, fp16_ok = 0;

    if (run_compute) {
        fprintf(stderr, "Running compute benchmark (%d iterations, %d workgroups)...",
                iterations, compute_size);
        fflush(stderr);
        if (bench_compute(iterations, &comp_gpu, &comp_cpu) == 0) {
            comp_ok = 1;
            fprintf(stderr, " OK (gpu avg: %.1f us)\n", stats_avg(&comp_gpu));
        } else {
            fprintf(stderr, " FAILED\n");
        }
    }

    if (run_graphics) {
        fprintf(stderr, "Running graphics benchmark (%d iterations, %d triangles)...",
                iterations, draw_count);
        fflush(stderr);
        if (bench_graphics(iterations, &gfx_gpu, &gfx_cpu) == 0) {
            gfx_ok = 1;
            fprintf(stderr, " OK (gpu avg: %.1f us)\n", stats_avg(&gfx_gpu));
        } else {
            fprintf(stderr, " FAILED\n");
        }
    }

    if (run_fp16) {
        fprintf(stderr, "Running FP16 vs FP32 throughput comparison (%d iterations)...",
                iterations);
        fflush(stderr);
        if (!g_vk.has_shader_float16) {
            fprintf(stderr, " SKIPPED (shaderFloat16 not supported)\n");
        } else if (bench_fp16_compute(iterations, &fp16_gpu, &fp16_cpu,
                                       &fp32_gpu, &fp32_cpu, &fp16_speedup) == 0) {
            fp16_ok = 1;
            fprintf(stderr, " OK (FP16 avg: %.1f us, FP32 avg: %.1f us, speedup: %.2fx)\n",
                    stats_avg(&fp16_gpu), stats_avg(&fp32_gpu), fp16_speedup);
        } else {
            fprintf(stderr, " FAILED\n");
        }
    }

    /* JSON output */
    fprintf(out, "{%s", nl);
    json_device_info(out);

    if (g_driver_so_path) {
        fprintf(out, "%s%s\"driver_so\": \"%s\",%s",
                compact ? "," : ",", nl, g_driver_so_path, nl);
    }

    fprintf(out, "%s%s\"benchmarks\": [%s", compact ? "," : ",", nl, nl);

    int first = 1;
    if (comp_ok) {
        if (!first) fprintf(out, ",%s", nl);
        json_benchmark(out, "compute", "compute", &comp_gpu, &comp_cpu,
                        iterations, NULL);
        first = 0;
    }
    if (gfx_ok) {
        if (!first) fprintf(out, ",%s", nl);
        char extra[256];
        snprintf(extra, sizeof(extra),
            "%s%s\"draw_calls\": 1,%s"
            "%s%s\"triangles\": %d",
            sp, sp, nl,
            sp, sp, draw_count);
        json_benchmark(out, "graphics", "graphics", &gfx_gpu, &gfx_cpu,
                        iterations, extra);
        first = 0;
    }
    if (fp16_ok) {
        if (!first) fprintf(out, ",%s", nl);
        char extra[256];
        snprintf(extra, sizeof(extra),
            "%s%s\"fp16_gpu_time_us\": %.2f,%s"
            "%s%s\"fp32_gpu_time_us\": %.2f,%s"
            "%s%s\"fp16_speedup\": %.2f,%s"
            "%s%s\"fp16_cpu_time_us\": %.2f,%s"
            "%s%s\"fp32_cpu_time_us\": %.2f",
            sp, sp, stats_median(&fp16_gpu), nl,
            sp, sp, stats_median(&fp32_gpu), nl,
            sp, sp, fp16_speedup, nl,
            sp, sp, stats_median(&fp16_cpu), nl,
            sp, sp, stats_median(&fp32_cpu));
        json_benchmark(out, "fp16-compute", "compute", &fp16_gpu, &fp16_cpu,
                        iterations, extra);
        first = 0;
    }

    fprintf(out, "%s%s]%s", nl, compact ? "" : sp, nl);
    fprintf(out, "}%s", nl);

    if (output_file) fclose(out);

    cleanup_icd();
    vk_cleanup();
    return (comp_ok || gfx_ok || fp16_ok) ? 0 : 1;
}
