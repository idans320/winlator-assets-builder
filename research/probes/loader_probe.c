#include <dlfcn.h>
#include <vulkan/vulkan.h>
#include <stdio.h>
#include <stdlib.h>

#define LOG(fmt,...) do{ fprintf(stderr, fmt "\n", ##__VA_ARGS__); fflush(stderr); }while(0)

int main(void) {
    setenv("TU_DEBUG", "trace", 1);
    LOG("loading...");
    void *icd = dlopen("/data/local/tmp/libvulkan_freedreno.so", RTLD_NOW);
    if (!icd) { LOG("dlopen fail"); return 1; }

    PFN_vkVoidFunction (*gp)(VkInstance,const char*) =
        (PFN_vkVoidFunction(*)(VkInstance,const char*))
        dlsym(icd, "vk_icdGetInstanceProcAddr");
    if (!gp) { LOG("no gp"); return 1; }

    PFN_vkCreateInstance ci = (PFN_vkCreateInstance)gp(NULL,"vkCreateInstance");
    if (!ci) { LOG("no ci"); return 1; }

    VkApplicationInfo ai = {VK_STRUCTURE_TYPE_APPLICATION_INFO,0,"p",1,0,VK_API_VERSION_1_2};
    VkInstanceCreateInfo ici = {VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO,0,0,&ai,0,0,0,0};
    VkInstance inst=0;
    if (ci(&ici,0,&inst) || !inst) { LOG("ci fail"); return 1; }
    LOG("inst created");

    PFN_vkDestroyInstance di = (PFN_vkDestroyInstance)gp(inst,"vkDestroyInstance");
    PFN_vkEnumeratePhysicalDevices ed = (PFN_vkEnumeratePhysicalDevices)gp(inst,"vkEnumeratePhysicalDevices");
    PFN_vkGetPhysicalDeviceQueueFamilyProperties gqf = (PFN_vkGetPhysicalDeviceQueueFamilyProperties)gp(inst,"vkGetPhysicalDeviceQueueFamilyProperties");
    PFN_vkCreateDevice cd = (PFN_vkCreateDevice)gp(inst,"vkCreateDevice");
    PFN_vkDestroyDevice dd = (PFN_vkDestroyDevice)gp(inst,"vkDestroyDevice");
    PFN_vkGetDeviceQueue gq = (PFN_vkGetDeviceQueue)gp(inst,"vkGetDeviceQueue");
    PFN_vkCreateCommandPool ccp = (PFN_vkCreateCommandPool)gp(inst,"vkCreateCommandPool");
    PFN_vkDestroyCommandPool dcp = (PFN_vkDestroyCommandPool)gp(inst,"vkDestroyCommandPool");
    PFN_vkAllocateCommandBuffers acb = (PFN_vkAllocateCommandBuffers)gp(inst,"vkAllocateCommandBuffers");
    PFN_vkBeginCommandBuffer bcb = (PFN_vkBeginCommandBuffer)gp(inst,"vkBeginCommandBuffer");
    PFN_vkEndCommandBuffer ecb = (PFN_vkEndCommandBuffer)gp(inst,"vkEndCommandBuffer");
    PFN_vkQueueSubmit qs = (PFN_vkQueueSubmit)gp(inst,"vkQueueSubmit");
    PFN_vkQueueWaitIdle qwi = (PFN_vkQueueWaitIdle)gp(inst,"vkQueueWaitIdle");
    PFN_vkGetPhysicalDeviceProperties gpp = (PFN_vkGetPhysicalDeviceProperties)gp(inst,"vkGetPhysicalDeviceProperties");

    if (!ed||!gqf||!cd||!dd) { LOG("missing fn"); return 1; }

    uint32_t nd=0; ed(inst,&nd,0);
    LOG("devs:%u", nd);
    VkPhysicalDevice pd; ed(inst,&nd,&pd);
    VkPhysicalDeviceProperties pr; gpp(pd,&pr);
    LOG("GPU:%s %d.%d.%d", pr.deviceName, VK_VERSION_MAJOR(pr.driverVersion), VK_VERSION_MINOR(pr.driverVersion), VK_VERSION_PATCH(pr.driverVersion));

    uint32_t qc=0; gqf(pd,&qc,0);
    VkQueueFamilyProperties qp[4]; gqf(pd,&qc,qp);
    uint32_t qf=UINT32_MAX;
    for (uint32_t i=0;i<qc&&i<4;i++) if(qp[i].queueFlags&VK_QUEUE_GRAPHICS_BIT){qf=i;break;}
    LOG("qfam:%u", qf);
    if(qf==UINT32_MAX)return 1;

    float prio=1.0f;
    VkDeviceQueueCreateInfo qci={VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO,0,0,qf,1,&prio};
    VkDeviceCreateInfo dci={VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO,0,0,1,&qci,0,0,0,0,0};
    VkDevice dev=0;
    if(cd(pd,&dci,0,&dev)||!dev){LOG("cd fail");return 1;}
    LOG("dev created");
    VkQueue q; gq(dev,qf,0,&q);
    LOG("queue=%p",(void*)q);

    VkCommandPoolCreateInfo cpi={VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO,0,0,qf};
    VkCommandPool cp=0;
    ccp(dev,&cpi,0,&cp);
    VkCommandBufferAllocateInfo cbai={VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO,0,cp,VK_COMMAND_BUFFER_LEVEL_PRIMARY,1};
    VkCommandBuffer cb=0;
    acb(dev,&cbai,&cb);
    LOG("cb=%p",(void*)cb);

    VkCommandBufferBeginInfo cbbi={VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO,0,VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT,0};
    LOG("begin cb...");
    bcb(cb,&cbbi);
    LOG("end cb...");
    ecb(cb);
    LOG("submit...");
    VkSubmitInfo si={VK_STRUCTURE_TYPE_SUBMIT_INFO,0,0,0,0,1,&cb,0,0};
    qs(q,1,&si,0);
    qwi(q);
    LOG("submit done");

    PFN_vkResetCommandBuffer rcb=(PFN_vkResetCommandBuffer)gp(inst,"vkResetCommandBuffer");
    rcb(cb,0);
    dcp(dev,cp,0);
    dd(dev,0);
    di(inst,0);
    dlclose(icd);
    LOG("done");
    return 0;
}
