// Performance benchmarks - Measure the impact of refactoring on performance
package data_store

import (
	"fmt"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// createBenchmarkMetrics creates a set of realistic metrics for benchmarking
func createBenchmarkMetrics(numMetrics int) []datapoint.DataPoint {
	metrics := make([]datapoint.DataPoint, 0, numMetrics)
	
	probes := []string{"cpu", "memory", "network", "redfish", "logicaldisk"}
	metricNames := []string{
		"cpu_usage_total", "cpu_user", "cpu_system",
		"memory_used_percent", "memory_available_bytes",
		"network_bytes_sent", "network_bytes_received",
		"thermal.cpu.0.temperature", "power.system.total",
		"logicaldisk_free_bytes", "logicaldisk_used_percent",
	}
	
	for i := 0; i < numMetrics; i++ {
		probe := probes[i%len(probes)]
		metricName := metricNames[i%len(metricNames)]
		
		metric := datapoint.DataPoint{
			Name:      metricName,
			Value:     float32(50.0 + float64(i%50)), // Realistic values 50-100
			Timestamp: time.Now().Add(-time.Duration(i) * time.Second),
			Tags: []tags.Tag{
				{Key: "probe_name", Value: probe},
				{Key: "instance", Value: fmt.Sprintf("%d", i%10)},
			},
		}
		
		// Add specific tags based on probe type
		switch probe {
		case "redfish":
			metric.Tags = append(metric.Tags, tags.Tag{Key: "index", Value: fmt.Sprintf("%d", i%4)})
		case "network":
			metric.Tags = append(metric.Tags, tags.Tag{Key: "interface", Value: fmt.Sprintf("eth%d", i%3)})
		case "logicaldisk":
			metric.Tags = append(metric.Tags, tags.Tag{Key: "drive", Value: fmt.Sprintf("C%d:", i%2)})
		}
		
		metrics = append(metrics, metric)
	}
	
	return metrics
}

// BenchmarkCacheInsertion benchmarks adding metrics to cache
func BenchmarkCacheInsertion(b *testing.B) {
	benchmarks := []struct {
		name      string
		numMetrics int
	}{
		{"10_metrics", 10},
		{"100_metrics", 100},
		{"1000_metrics", 1000},
		{"10000_metrics", 10000},
	}
	
	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			// Setup
			baseLogger := createTestLogger()
			moduleLogger := logger.NewModuleLogger(baseLogger, "benchmark")
			cache := NewMetricCache(5*time.Minute, moduleLogger)
			transformerRegistry := transformers.NewTransformerRegistry(baseLogger)
			
			// Create test data once
			testMetrics := createBenchmarkMetrics(bm.numMetrics)
			
			b.ResetTimer()
			b.ReportAllocs()
			
			// Benchmark the insertion
			for i := 0; i < b.N; i++ {
				// Clear cache for each iteration
				cache.mu.Lock()
				cache.timeSeries = make(map[string]CachedMetric)
				cache.probeIndex = make(map[string]map[string]bool)
				cache.mu.Unlock()
				
				// Insert metrics
				cache.AddDataPointsWithTransformer(testMetrics, transformerRegistry)
			}
		})
	}
}

// BenchmarkFormatConversion benchmarks the performance of format conversion
func BenchmarkFormatConversion(b *testing.B) {
	// Setup with realistic data size
	baseLogger := createTestLogger()
	moduleLogger := logger.NewModuleLogger(baseLogger, "benchmark")
	cache := NewMetricCache(5*time.Minute, moduleLogger)
	transformerRegistry := transformers.NewTransformerRegistry(baseLogger)
	formatConverter := NewFormatConverter(transformerRegistry, moduleLogger, cache)
	
	// Add test data
	testMetrics := createBenchmarkMetrics(1000)
	cache.AddDataPointsWithTransformer(testMetrics, transformerRegistry)
	
	b.Run("SenHub_Format", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()
		
		for i := 0; i < b.N; i++ {
			_ = formatConverter.GetSenHubMetricsForProbe("cpu")
		}
	})
	
	b.Run("PRTG_Format", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()
		
		for i := 0; i < b.N; i++ {
			_ = formatConverter.GetMetricsForProbe("cpu")
		}
	})
	
	b.Run("Nagios_Format", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()
		
		for i := 0; i < b.N; i++ {
			_ = formatConverter.GetNagiosMetricsForProbe("cpu")
		}
	})
}

// BenchmarkCacheRetrieval benchmarks retrieving metrics from cache
func BenchmarkCacheRetrieval(b *testing.B) {
	// Setup with different cache sizes
	cacheSizes := []int{100, 1000, 10000}
	
	for _, size := range cacheSizes {
		b.Run(fmt.Sprintf("Cache_%d_metrics", size), func(b *testing.B) {
			// Setup
			baseLogger := createTestLogger()
			moduleLogger := logger.NewModuleLogger(baseLogger, "benchmark")
			cache := NewMetricCache(5*time.Minute, moduleLogger)
			transformerRegistry := transformers.NewTransformerRegistry(baseLogger)
			
			// Populate cache
			testMetrics := createBenchmarkMetrics(size)
			cache.AddDataPointsWithTransformer(testMetrics, transformerRegistry)
			
			b.ResetTimer()
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				// Rotate through different probes
				probe := []string{"cpu", "memory", "network", "redfish", "logicaldisk"}[i%5]
				_ = cache.GetProbeMetrics(probe)
			}
		})
	}
}

// BenchmarkTransformerPerformance benchmarks transformer performance
func BenchmarkTransformerPerformance(b *testing.B) {
	baseLogger := createTestLogger()
	transformerRegistry := transformers.NewTransformerRegistry(baseLogger)
	
	// Test different probe types
	probeTypes := []string{"cpu", "memory", "network", "redfish", "host"}
	metricNames := []string{
		"cpu_usage_total", "memory_used_percent", "network_bytes_sent",
		"thermal.cpu.0.temperature", "logicaldisk_free_bytes",
	}
	
	for i, probe := range probeTypes {
		b.Run(fmt.Sprintf("Transform_%s", probe), func(b *testing.B) {
			metricName := metricNames[i%len(metricNames)]
			tags := map[string]string{
				"instance": "0",
				"probe_name": probe,
			}
			
			// Load transformer once
			transformer, err := transformerRegistry.LoadTransformer(probe, "friendly")
			if err != nil {
				b.Skipf("No transformer for probe %s: %v", probe, err)
			}
			
			b.ResetTimer()
			b.ReportAllocs()
			
			for j := 0; j < b.N; j++ {
				_ = transformer.TransformMetricName(metricName, tags)
				_ = transformer.GetUnit(metricName)
			}
		})
	}
}

// BenchmarkConcurrentAccess benchmarks concurrent access to cache
func BenchmarkConcurrentAccess(b *testing.B) {
	// Setup
	baseLogger := createTestLogger()
	moduleLogger := logger.NewModuleLogger(baseLogger, "benchmark")
	cache := NewMetricCache(5*time.Minute, moduleLogger)
	transformerRegistry := transformers.NewTransformerRegistry(baseLogger)
	formatConverter := NewFormatConverter(transformerRegistry, moduleLogger, cache)
	
	// Populate cache
	testMetrics := createBenchmarkMetrics(1000)
	cache.AddDataPointsWithTransformer(testMetrics, transformerRegistry)
	
	b.Run("Concurrent_Reads", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()
		
		b.RunParallel(func(pb *testing.PB) {
			probes := []string{"cpu", "memory", "network", "redfish", "logicaldisk"}
			i := 0
			for pb.Next() {
				probe := probes[i%len(probes)]
				_ = formatConverter.GetSenHubMetricsForProbe(probe)
				i++
			}
		})
	})
	
	b.Run("Mixed_Read_Write", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()
		
		b.RunParallel(func(pb *testing.PB) {
			probes := []string{"cpu", "memory", "network", "redfish", "logicaldisk"}
			i := 0
			for pb.Next() {
				if i%10 == 0 {
					// Occasional write (10% of operations)
					newMetrics := createBenchmarkMetrics(10)
					cache.AddDataPointsWithTransformer(newMetrics, transformerRegistry)
				} else {
					// Mostly reads (90% of operations)
					probe := probes[i%len(probes)]
					_ = formatConverter.GetSenHubMetricsForProbe(probe)
				}
				i++
			}
		})
	})
}

// BenchmarkFilteringPerformance benchmarks metric filtering performance
func BenchmarkFilteringPerformance(b *testing.B) {
	// Setup
	baseLogger := createTestLogger()
	moduleLogger := logger.NewModuleLogger(baseLogger, "benchmark")
	cache := NewMetricCache(5*time.Minute, moduleLogger)
	transformerRegistry := transformers.NewTransformerRegistry(baseLogger)
	formatConverter := NewFormatConverter(transformerRegistry, moduleLogger, cache)
	
	// Add test data with various tags
	testMetrics := createBenchmarkMetrics(1000)
	cache.AddDataPointsWithTransformer(testMetrics, transformerRegistry)
	
	filters := []struct {
		name   string
		filter MetricFilter
	}{
		{
			name: "No_Filter",
			filter: MetricFilter{},
		},
		{
			name: "Tag_Filter",
			filter: MetricFilter{
				TagFilters: map[string][]string{
					"instance": {"0", "1", "2"},
				},
			},
		},
		{
			name: "Metric_Name_Filter",
			filter: MetricFilter{
				MetricNames: []string{"cpu_usage_total", "memory_used_percent"},
			},
		},
		{
			name: "Complex_Filter",
			filter: MetricFilter{
				TagFilters: map[string][]string{
					"instance": {"0", "1"},
				},
				MetricNames: []string{"cpu_usage_total"},
				Limit:       50,
				Offset:      10,
			},
		},
	}
	
	for _, filter := range filters {
		b.Run(filter.name, func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				_ = formatConverter.GetMetricsForProbeWithFilter("cpu", filter.filter)
			}
		})
	}
}

// BenchmarkMemoryUsage provides insights into memory consumption
func BenchmarkMemoryUsage(b *testing.B) {
	metricCounts := []int{100, 1000, 10000}
	
	for _, count := range metricCounts {
		b.Run(fmt.Sprintf("Memory_%d_metrics", count), func(b *testing.B) {
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				// Setup fresh cache for each iteration
				baseLogger := createTestLogger()
				moduleLogger := logger.NewModuleLogger(baseLogger, "benchmark")
				cache := NewMetricCache(5*time.Minute, moduleLogger)
				transformerRegistry := transformers.NewTransformerRegistry(baseLogger)
				
				// Add metrics
				testMetrics := createBenchmarkMetrics(count)
				cache.AddDataPointsWithTransformer(testMetrics, transformerRegistry)
				
				// Force garbage collection to measure actual retained memory
				// runtime.GC()
			}
		})
	}
}