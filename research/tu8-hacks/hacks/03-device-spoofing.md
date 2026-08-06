# Change 03: Device & Driver Spoofing

**Files:** `tu_device.cc` (+40/-5), `tu_util.cc` (+1), `tu_util.h` (+1), `tu_version.h` (new)

## Steam Deck Emulation (`TU_DEBUG=deck_emu`)

The fork can impersonate an AMD Steam Deck GPU (Van Gogh / RADV):

```c
if (TU_DEBUG(DECK_EMU)) {
    p->driverID = VK_DRIVER_ID_MESA_RADV;
    memset(p->driverName, 0, sizeof(p->driverName));
    snprintf(p->driverName, VK_MAX_DRIVER_NAME_SIZE, "radv");
    props->vendorID = 0x1002;          // AMD
    props->deviceID = 0x163F;          // Van Gogh APU
    strcpy(props->deviceName, "AMD Custom GPU 0405 (RADV VANGOGH)");
}
```

### Why?
Many Windows games (via DXVK/VKD3D) check the GPU vendor/device ID and refuse to
run on non-AMD/non-NVIDIA GPUs. The Steam Deck (Van Gogh) is widely whitelisted.

### Activation
```bash
TU_DEBUG=deck_emu ./vulkan-app
```

## Forced Vulkan 1.3

The fork overrides Mesa's conservative API version exposure:

```c
// Vanilla: only VK 1.3 if multiview supported
props->apiVersion = tu_has_multiview(pdevice)
    ? VK_MAKE_VERSION(1, 3, VK_HEADER_VERSION)
    : VK_MAKE_VERSION(1, 0, VK_HEADER_VERSION);

// Fork: VK 1.3 on all gen7+ devices
props->apiVersion = pdevice->info->chip >= 7
    ? TU_API_VERSION
    : VK_MAKE_VERSION(1, 3, VK_HEADER_VERSION);
```

### Why?
> "Minecraft renderer checks for VK1.2 presence and refuses to start on VK1.0"

The upstream check `tu_has_multiview()` returns false on many Adreno devices that
don't support multiview, causing them to report Vulkan 1.0. This breaks games
that require Vulkan 1.2+.

### Conformance Note
> "This is not conformant, but I don't care. Sigh."

The fork knowingly violates Vulkan conformance — multiview is required for
Vulkan 1.1+, and exposing 1.3 without it means `VK_KHR_multiview` is
advertised but not functional.

## Driver Branding

- **Driver name:** `"turnip Mesa driver"` → `"turnip Mesa driver (whitebelyash branch)"`
- **Device name:** Appended with `(TUGEN8_DRV_VERSION)` from `tu_version.h`
- **tu_version.h:** New file containing `#define TUGEN8_DRV_VERSION "..."`

These branding changes make it easy to identify which driver build is running
in logs and game overlays.

## Concurrent Binning Disabled

```c
tu_env.debug |= TU_DEBUG_NO_CONCURRENT_BINNING;
```

Always sets the `nocb` debug flag. Concurrent binning (running the binning pass
concurrently with previous renderpass resolve) is disabled because it causes
instability on some A8XX devices.

## Debug Flag Addition

New debug enum value `TU_DEBUG_DECK_EMU` (bit 37) in `tu_util.h`:
```c
TU_DEBUG_DECK_EMU = BITFIELD64_BIT(37),
```

Registered as `"deck_emu"` in the debug flag table. Note: bit 37 collides with
`TU_DEBUG_COMPUTE_ROUND_ROBIN` — this is a bug (both use bit 37). The flags
cannot be used together.
