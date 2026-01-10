package runtime_test

import (
	"testing"

	"github.com/jtarchie/ci/runtime"
)

// Simple TypeScript pipeline for benchmarking transpilation
const simplePipeline = `
const pipeline = async () => {
  await runtime.run({
    name: "test",
    image: "busybox",
    command: { path: "echo", args: ["hello"] },
  });
};
export { pipeline };
`

// Complex TypeScript pipeline with multiple tasks
const complexPipeline = `
const pipeline = async () => {
  const results: string[] = [];
  
  for (let i = 0; i < 10; i++) {
    const result = await runtime.run({
      name: "task-" + i,
      image: "busybox",
      command: { path: "echo", args: ["hello " + i] },
      env: { INDEX: String(i), FOO: "bar", BAZ: "qux" },
    });
    results.push(result.stdout);
  }
  
  await Promise.all([
    runtime.run({ name: "parallel-1", image: "busybox", command: { path: "true" } }),
    runtime.run({ name: "parallel-2", image: "busybox", command: { path: "true" } }),
    runtime.run({ name: "parallel-3", image: "busybox", command: { path: "true" } }),
  ]);
  
  return results;
};
export { pipeline };
`

func BenchmarkTranspileAndValidate_Simple(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		_, err := runtime.TranspileAndValidate(simplePipeline)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTranspileAndValidate_Complex(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		_, err := runtime.TranspileAndValidate(complexPipeline)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkTranspileAndValidate_Parallel tests concurrent transpilation
func BenchmarkTranspileAndValidate_Parallel(b *testing.B) {
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := runtime.TranspileAndValidate(simplePipeline)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
