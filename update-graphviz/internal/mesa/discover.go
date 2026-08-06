package mesa

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var ccFiles = []string{
	"tu_cmd_buffer.cc", "tu_clear_blit.cc", "tu_cs.cc", "tu_cs_breadcrumbs.cc",
	"tu_descriptor_set.cc", "tu_device.cc", "tu_dynamic_rendering.cc", "tu_event.cc",
	"tu_formats.cc", "tu_image.cc", "tu_knl.cc", "tu_knl_drm.cc",
	"tu_knl_drm_msm.cc", "tu_knl_drm_virtio.cc", "tu_knl_kgsl.cc", "tu_lrz.cc",
	"tu_pass.cc", "tu_pipeline.cc", "tu_query_pool.cc", "tu_queue.cc",
	"tu_sampler.cc", "tu_shader.cc", "tu_suballoc.cc", "tu_tile_config.cc",
	"tu_util.cc", "tu_wsi.cc",
}

var hFiles = []string{
	"tu_cmd_buffer.h", "tu_clear_blit.h", "tu_common.h", "tu_cs.h",
	"tu_descriptor_set.h", "tu_device.h", "tu_dynamic_rendering.h", "tu_event.h",
	"tu_formats.h", "tu_image.h", "tu_knl.h", "tu_lrz.h",
	"tu_pass.h", "tu_pipeline.h", "tu_query_pool.h", "tu_queue.h",
	"tu_sampler.h", "tu_shader.h", "tu_suballoc.h", "tu_tile_config.h",
	"tu_util.h", "tu_version.h", "tu_wsi.h",
}

var gen8PatternRe = regexp.MustCompile(`(?i)\b(CHIP\s*[<>=!]+\s*A8XX|A8XX_\w+|gen8|a8xx)\b`)

func DiscoverCCFiles(root string) []string {
	vulkanDir := filepath.Join(root, "src", "freedreno", "vulkan")
	var files []string
	for _, name := range ccFiles {
		p := filepath.Join(vulkanDir, name)
		if _, err := os.Stat(p); err == nil && HasGen8Content(p) {
			files = append(files, p)
		}
	}
	if !HasGen8Content(filepath.Join(vulkanDir, "tu_knl_kgsl.cc")) {
		if _, err := os.Stat(filepath.Join(vulkanDir, "tu_knl_kgsl.cc")); err == nil {
			files = append(files, filepath.Join(vulkanDir, "tu_knl_kgsl.cc"))
		}
	}
	if !HasGen8Content(filepath.Join(vulkanDir, "tu_cs.cc")) {
		if _, err := os.Stat(filepath.Join(vulkanDir, "tu_cs.cc")); err == nil {
			files = append(files, filepath.Join(vulkanDir, "tu_cs.cc"))
		}
	}
	return files
}

func DiscoverHFiles(root string) []string {
	vulkanDir := filepath.Join(root, "src", "freedreno", "vulkan")
	var files []string
	for _, name := range hFiles {
		p := filepath.Join(vulkanDir, name)
		if _, err := os.Stat(p); err == nil && HasGen8Content(p) {
			files = append(files, p)
		}
	}
	if !HasGen8Content(filepath.Join(vulkanDir, "tu_descriptor_set.h")) {
		if _, err := os.Stat(filepath.Join(vulkanDir, "tu_descriptor_set.h")); err == nil {
			files = append(files, filepath.Join(vulkanDir, "tu_descriptor_set.h"))
		}
	}
	return files
}

func HasGen8Content(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return gen8PatternRe.Match(data)
}

func ContainsGen8(content string) bool {
	return gen8PatternRe.MatchString(content) || strings.Contains(content, "CHIP >=") || strings.Contains(content, "CHIP ==")
}
