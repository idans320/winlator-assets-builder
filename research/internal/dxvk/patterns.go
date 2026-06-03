package dxvk

import (
	"os"
	"regexp"
	"strings"
)

// DiscoverCCFiles finds all DXVK C++ source files under root/src/.
func DiscoverCCFiles(root string) []string {
	var files []string

	dirs := []string{
		root + "/dxvk",
		root + "/d3d11",
		root + "/d3d9",
		root + "/d3d8",
		root + "/dxbc",
		root + "/spirv",
		root + "/dxgi",
	}

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if strings.HasSuffix(name, ".cpp") || strings.HasSuffix(name, ".h") {
				files = append(files, dir+"/"+name)
			}
		}
	}
	return files
}

// VkCallPattern matches Vulkan API call sites — the "ejection points" from DXVK to GPU driver.
var vkCallPatterns = []*regexp.Regexp{
	regexp.MustCompile(`vk(Cmd|Queue|Allocate|Create|Destroy|Bind|Set|Get|Map|Unmap|Flush|Invalidate|Wait|Signal|Reset|Free|Begin|End|Next|Enumerate|Merge|Update|WriteTimestamp)\w+\(`),
	regexp.MustCompile(`m_cmd->cmd\w+\(`),
	regexp.MustCompile(`cmdPushConstants\(`),
	regexp.MustCompile(`cmdBindPipeline\(`),
	regexp.MustCompile(`cmdDraw\w*\(`),
	regexp.MustCompile(`cmdDispatch\w*\(`),
}

// DxvkContextCallPattern matches internal DXVK context hot-path methods.
var dxvkContextCallPatterns = []*regexp.Regexp{
	regexp.MustCompile(`commitGraphicsState\b`),
	regexp.MustCompile(`commitComputeState\b`),
	regexp.MustCompile(`updateGraphicsPipeline\b`),
	regexp.MustCompile(`updateComputePipeline\b`),
	regexp.MustCompile(`startRenderPass\b`),
	regexp.MustCompile(`spillRenderPass\b`),
	regexp.MustCompile(`updateGraphicsShaderResources\b`),
	regexp.MustCompile(`updateComputeShaderResources\b`),
	regexp.MustCompile(`flushClears\b`),
	regexp.MustCompile(`deferClear\b`),
	regexp.MustCompile(`emitGraphicsBarrier\b`),
	regexp.MustCompile(`emitBufferBarrier\b`),
	regexp.MustCompile(`emitImageBarrier\b`),
	regexp.MustCompile(`checkGraphicsHazards\b`),
	regexp.MustCompile(`checkComputeHazards\b`),
	regexp.MustCompile(`bindRenderTargets\b`),
	regexp.MustCompile(`bindVertexBuffer\b`),
	regexp.MustCompile(`bindIndexBuffer\b`),
	regexp.MustCompile(`bindUniformBuffer\b`),
	regexp.MustCompile(`bindShader\b`),
	regexp.MustCompile(`pushData\b`),
	regexp.MustCompile(`updateBuffer\b`),
	regexp.MustCompile(`uploadBuffer\b`),
	regexp.MustCompile(`uploadImage\b`),
}

// D3DEntryPattern matches D3D API entry points.
var d3dEntryPatterns = []*regexp.Regexp{
	regexp.MustCompile(`STDMETHODCALLTYPE\s+(Draw\w+)\(`),
	regexp.MustCompile(`STDMETHODCALLTYPE\s+(Dispatch\w*)\(`),
	regexp.MustCompile(`STDMETHODCALLTYPE\s+(\w*Set\w+)\(`),
	regexp.MustCompile(`STDMETHODCALLTYPE\s+(\w*Copy\w+)\(`),
	regexp.MustCompile(`STDMETHODCALLTYPE\s+(\w*Clear\w+)\(`),
	regexp.MustCompile(`STDMETHODCALLTYPE\s+(\w*Update\w+)\(`),
	regexp.MustCompile(`STDMETHODCALLTYPE\s+(\w*Map\w*)\(`),
	regexp.MustCompile(`STDMETHODCALLTYPE\s+(\w*Create\w+)\(`),
	regexp.MustCompile(`D3D9DeviceEx\)\s+(\w+)\(`),
	regexp.MustCompile(`D3D11Device\)\s+(\w+)\(`),
}

// OptimizationTargetPatterns matches code patterns that are optimization candidates.
var optimizationTargetPatterns = []struct {
	Name    string
	Pattern *regexp.Regexp
}{
	{Name: "state-cache-lookup", Pattern: regexp.MustCompile(`m_gpLookupCache\b`)},
	{Name: "state-cache-lookup", Pattern: regexp.MustCompile(`m_cpLookupCache\b`)},
	{Name: "descriptor-update", Pattern: regexp.MustCompile(`updateShaderResources\b`)},
	{Name: "descriptor-update", Pattern: regexp.MustCompile(`cmdBindDescriptorSets\b`)},
	{Name: "pipeline-compile", Pattern: regexp.MustCompile(`compilePipeline\b`)},
	{Name: "pipeline-compile", Pattern: regexp.MustCompile(`updateGraphicsPipeline\b`)},
	{Name: "renderpass-spill", Pattern: regexp.MustCompile(`spillRenderPass\b`)},
	{Name: "renderpass-start", Pattern: regexp.MustCompile(`startRenderPass\b`)},
	{Name: "barrier-emit", Pattern: regexp.MustCompile(`emitGraphicsBarrier\b`)},
	{Name: "barrier-emit", Pattern: regexp.MustCompile(`emitBufferBarrier\b`)},
	{Name: "barrier-emit", Pattern: regexp.MustCompile(`cmdPipelineBarrier\b`)},
	{Name: "clear-deferral", Pattern: regexp.MustCompile(`deferClear\b`)},
	{Name: "clear-deferral", Pattern: regexp.MustCompile(`flushClears\b`)},
	{Name: "buffer-invalidation", Pattern: regexp.MustCompile(`invalidateBuffer\b`)},
	{Name: "buffer-upload", Pattern: regexp.MustCompile(`uploadBuffer\b`)},
	{Name: "buffer-upload", Pattern: regexp.MustCompile(`uploadImage\b`)},
	{Name: "copy-operation", Pattern: regexp.MustCompile(`copyBuffer\b`)},
	{Name: "copy-operation", Pattern: regexp.MustCompile(`copyImage\b`)},
	{Name: "hazard-check", Pattern: regexp.MustCompile(`checkGraphicsHazards\b`)},
	{Name: "hazard-check", Pattern: regexp.MustCompile(`checkComputeHazards\b`)},
	{Name: "cs-chunk-flush", Pattern: regexp.MustCompile(`EmitCs\b`)},
	{Name: "cs-chunk-flush", Pattern: regexp.MustCompile(`FlushCsChunk\b`)},
	{Name: "cs-chunk-flush", Pattern: regexp.MustCompile(`ConsiderFlush\b`)},
	{Name: "dxbc-compile", Pattern: regexp.MustCompile(`DxbcCompiler\b`)},
	{Name: "dxbc-compile", Pattern: regexp.MustCompile(`compileShader\b`)},
	{Name: "spirv-emit", Pattern: regexp.MustCompile(`SpirvModule\b`)},
	{Name: "present-latency", Pattern: regexp.MustCompile(`beginLatencyTracking\b`)},
	{Name: "present-latency", Pattern: regexp.MustCompile(`endLatencyTracking\b`)},
	{Name: "present-latency", Pattern: regexp.MustCompile(`DxvkLatencyTracker\b`)},
}

// Map DxvkContext public method to a category for bottleneck analysis.
var contextMethodCategories = map[string]string{
	"draw":                         "drawcall",
	"drawIndirect":                 "drawcall",
	"drawIndexed":                  "drawcall",
	"drawIndexedIndirect":          "drawcall",
	"dispatch":                     "compute",
	"dispatchIndirect":             "compute",
	"bindShader":                   "state-binding",
	"bindRenderTargets":            "state-binding",
	"bindVertexBuffer":             "state-binding",
	"bindIndexBuffer":              "state-binding",
	"bindUniformBuffer":            "state-binding",
	"bindResourceImageView":        "state-binding",
	"bindResourceSampler":          "state-binding",
	"setViewports":                 "dynamic-state",
	"setScissor":                   "dynamic-state",
	"setBlendConstants":            "dynamic-state",
	"setDepthBias":                 "dynamic-state",
	"setStencilReference":          "dynamic-state",
	"setInputAssemblyState":        "pipeline-state",
	"setInputLayout":               "pipeline-state",
	"setRasterizerState":           "pipeline-state",
	"setDepthStencilState":         "pipeline-state",
	"setBlendMode":                 "pipeline-state",
	"clearBuffer":                  "clear",
	"clearRenderTarget":            "clear",
	"clearImageView":               "clear",
	"copyBuffer":                   "copy",
	"copyImage":                    "copy",
	"copyBufferToImage":            "copy",
	"blitImageView":                "blit",
	"resolveImage":                 "resolve",
	"generateMipmaps":              "mipgen",
	"updateBuffer":                 "upload",
	"uploadBuffer":                 "upload",
	"uploadImage":                  "upload",
	"invalidateBuffer":             "invalidation",
	"invalidateImage":              "invalidation",
	"pushData":                     "pushconstant",
	"signalGpuEvent":               "sync",
	"writeTimestamp":               "sync",
	"emitGraphicsBarrier":          "barrier",
	"emitBufferBarrier":            "barrier",
	"emitImageBarrier":             "barrier",
	"beginRecording":               "recording",
	"endRecording":                 "recording",
	"flushCommandList":             "flush",
	"beginExternalRendering":       "present",
	"endFrame":                     "present",
}

// GetContextMethodCategory returns the category for a DxvkContext method.
func GetContextMethodCategory(method string) string {
	if cat, ok := contextMethodCategories[method]; ok {
		return cat
	}
	return "other"
}

// ContextMethodCategories returns the full method→category map for iteration.
func ContextMethodCategories() map[string]string {
	return contextMethodCategories
}

// IsVulkanEjectionPoint returns true if the line matches VK API call patterns.
func IsVulkanEjectionPoint(line string) bool {
	for _, re := range vkCallPatterns {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

// IsContextHotPath returns true if the line matches a DXVK context hot-path call.
func IsContextHotPath(line string) bool {
	for _, re := range dxvkContextCallPatterns {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

// FindOptimizationTargets scans a line for optimization patterns and returns matching names.
func FindOptimizationTargets(line string) []string {
	var targets []string
	for _, t := range optimizationTargetPatterns {
		if t.Pattern.MatchString(line) {
			targets = append(targets, t.Name)
		}
	}
	return targets
}
