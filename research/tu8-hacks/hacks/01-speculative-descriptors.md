# Change 01: Speculative Descriptor Access

**File:** `tu_shader.cc` (+158/-11)  
**Also:** `tu_descriptor_set.cc`, `tu_descriptor_set.h`, `tu_subsampled_image.cc`

## Vanilla Behavior

In upstream Mesa, all bindless descriptor loads are emitted with `ACCESS_CAN_SPECULATE = 0`:

```c
nir_def *bindless = nir_bindless_resource_ir3(b, 32, desc_offset,
    .desc_set = set);
```

The GPU must resolve all descriptor index calculations before fetching, causing
pipeline stalls on every texture/image/buffer access.

## Fork Change

Adds speculative access tagging based on compile-time descriptor index analysis:

```c
bool can_speculate_descriptor = true;

if (deref->deref_type == nir_deref_type_array) {
    if (!nir_src_is_const(deref->arr.index) ||      // dynamic index → no speculation
        (set_layout->has_variable_descriptors &&      // variable count → no speculation
         binding == set_layout->binding_count - 1) ||
        nir_src_as_uint(deref->arr.index) >= bind_layout->array_size)  // OOB → no speculation
        can_speculate_descriptor = false;
}

*descriptor_valid = !bind_layout->partially_bound && can_speculate_descriptor;

nir_def *bindless = nir_bindless_resource_ir3(b, 32, desc_offset,
    .desc_set = set,
    .access = can_speculate_descriptor ? ACCESS_CAN_SPECULATE : 0);
```

## When Speculation Is Safe

| Condition | Speculation? | Reason |
|-----------|-------------|--------|
| Static array index within bounds | Yes | Index known at compile time, descriptor valid |
| Dynamic array index | No | Index unknown at compile time |
| Variable descriptor count binding | No | Binding may have fewer descriptors than accessed |
| Array index >= binding array_size | No | Out-of-bounds access |
| Partially bound binding | No | Some descriptors may be VK_NULL_HANDLE |

## Image Instruction Speculation

For image load, sparse load, size, and samples instructions, the fork also adds
speculation to the NIR intrinsic when the descriptor is valid:

```c
if ((instr->intrinsic == nir_intrinsic_image_deref_load ||
     instr->intrinsic == nir_intrinsic_image_deref_sparse_load ||
     instr->intrinsic == nir_intrinsic_image_size ||
     instr->intrinsic == nir_intrinsic_image_samples) && descriptor_valid) {
    nir_intrinsic_set_access(instr, ACCESS_CAN_SPECULATE | nir_intrinsic_access(instr));
}
```

## Subsampled Image Support

`tu_get_subsampled_coordinates()` gains a `can_speculate` parameter that propagates
to UBO loads for subsampled metadata:

```c
nir_def *hdr0 = nir_load_ubo(b, 4, 32, descriptor, offset,
    .access = can_speculate ? ACCESS_CAN_SPECULATE : 0, ...);
```

The caller (`tu_shader.cc`) determines speculation safety from the descriptor
binding analysis.

## Partially-Bound Descriptor Metadata

New field in `tu_descriptor_set_binding_layout`:

```c
bool partially_bound;
```

Set when:
- `VK_DESCRIPTOR_SET_LAYOUT_CREATE_DESCRIPTOR_BUFFER_BIT_EXT` is used (implied PARTIALLY_BOUND)
- `VK_DESCRIPTOR_BINDING_PARTIALLY_BOUND_BIT` flag is set

This field is also included in the pipeline cache BLAKE3 hash to prevent collisions
between layouts that differ in partial-bind status.

## Performance Impact

Descriptor loads are one of the most common GPU pipeline stall points. Allowing
the GPU to speculatively prefetch descriptors:
- Reduces texture fetch latency by hiding descriptor load behind other work
- Especially effective in descriptor-heavy workloads (DXVK binds many UBO/tex descriptors)
- Zero correctness risk — speculation is only enabled when the compiler proves safety
