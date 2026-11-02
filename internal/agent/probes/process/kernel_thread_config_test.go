package process

import "testing"

func TestIncludeKernelThreadsIsOffByDefault(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.includeKernelThreads {
		t.Fatal("kernel threads are reported by default")
	}
	cfg, err = parseConfig(map[string]interface{}{"include_kernel_threads": true})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.includeKernelThreads {
		t.Fatal("include_kernel_threads: true is ignored")
	}
}
