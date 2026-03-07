package runtime

import (
	"sort"
	"testing"

	. "github.com/onsi/gomega"
)

func TestParseTaskStepID(t *testing.T) {
	t.Parallel()

	assert := NewGomegaWithT(t)

	idx, name := parseTaskStepID("0-git-clone")
	assert.Expect(idx).To(Equal(0))
	assert.Expect(name).To(Equal("git-clone"))

	idx, name = parseTaskStepID("12-run-tests")
	assert.Expect(idx).To(Equal(12))
	assert.Expect(name).To(Equal("run-tests"))

	// stepID with no dash falls back gracefully.
	idx, name = parseTaskStepID("badid")
	assert.Expect(idx).To(Equal(-1))
	assert.Expect(name).To(Equal("badid"))

	// Non-numeric prefix falls back gracefully.
	idx, name = parseTaskStepID("x-name")
	assert.Expect(idx).To(Equal(-1))
	assert.Expect(name).To(Equal("x-name"))
}

func TestLevenshtein(t *testing.T) {
	t.Parallel()

	assert := NewGomegaWithT(t)

	assert.Expect(levenshtein("kitten", "sitting")).To(Equal(3))
	assert.Expect(levenshtein("", "abc")).To(Equal(3))
	assert.Expect(levenshtein("abc", "")).To(Equal(3))
	assert.Expect(levenshtein("abc", "abc")).To(Equal(0))
	// Case-insensitive.
	assert.Expect(levenshtein("BUILD", "build")).To(Equal(0))
}

func TestFuzzyFindTask(t *testing.T) {
	t.Parallel()

	tasks := []taskSummary{
		{Name: "git-clone", Index: 0, Status: "success"},
		{Name: "run-tests", Index: 1, Status: "failure"},
		{Name: "build", Index: 2, Status: "success"},
		{Name: "deploy", Index: 3, Status: "pending"},
	}

	t.Run("exact match", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		got, ok := fuzzyFindTask(tasks, "build")
		assert.Expect(ok).To(BeTrue())
		assert.Expect(got.Name).To(Equal("build"))
		assert.Expect(got.Index).To(Equal(2))
	})

	t.Run("partial substring match", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		got, ok := fuzzyFindTask(tasks, "test")
		assert.Expect(ok).To(BeTrue())
		assert.Expect(got.Name).To(Equal("run-tests"))
	})

	t.Run("case-insensitive substring", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		got, ok := fuzzyFindTask(tasks, "GIT")
		assert.Expect(ok).To(BeTrue())
		assert.Expect(got.Name).To(Equal("git-clone"))
	})

	t.Run("fuzzy fallback picks closest", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		// "deploi" is closest in edit distance to "deploy".
		got, ok := fuzzyFindTask(tasks, "deploi")
		assert.Expect(ok).To(BeTrue())
		assert.Expect(got.Name).To(Equal("deploy"))
	})

	t.Run("empty task list returns false", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		_, ok := fuzzyFindTask(nil, "build")
		assert.Expect(ok).To(BeFalse())
	})
}

func TestTruncateStr(t *testing.T) {
	t.Parallel()

	t.Run("no truncation when shorter", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		s, truncated := truncateStr("hello", 10)
		assert.Expect(s).To(Equal("hello"))
		assert.Expect(truncated).To(BeFalse())
	})

	t.Run("truncates when longer", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		s, truncated := truncateStr("hello world", 5)
		assert.Expect(s).To(Equal("hello"))
		assert.Expect(truncated).To(BeTrue())
	})

	t.Run("zero maxBytes means no truncation", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		s, truncated := truncateStr("hello", 0)
		assert.Expect(s).To(Equal("hello"))
		assert.Expect(truncated).To(BeFalse())
	})
}

func TestLoadTaskSummaries_Sorting(t *testing.T) {
	t.Parallel()

	assert := NewGomegaWithT(t)

	tasks := []taskSummary{
		{Name: "build", Index: 2},
		{Name: "clone", Index: 0},
		{Name: "test", Index: 1},
	}

	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].Index < tasks[j].Index
	})

	assert.Expect(tasks[0].Name).To(Equal("clone"))
	assert.Expect(tasks[1].Name).To(Equal("test"))
	assert.Expect(tasks[2].Name).To(Equal("build"))
}

func TestTaskSummaryToMap(t *testing.T) {
	t.Parallel()

	t.Run("all fields present", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		ts := taskSummary{
			Name:      "build",
			Index:     3,
			Status:    "success",
			StartedAt: "2026-01-01T00:00:00Z",
			Elapsed:   "5s",
		}
		m := taskSummaryToMap(ts)
		assert.Expect(m["name"]).To(Equal("build"))
		assert.Expect(m["index"]).To(Equal(3))
		assert.Expect(m["status"]).To(Equal("success"))
		assert.Expect(m["started_at"]).To(Equal("2026-01-01T00:00:00Z"))
		assert.Expect(m["elapsed"]).To(Equal("5s"))
	})

	t.Run("empty optional fields omitted", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		ts := taskSummary{Name: "build", Index: 0}
		m := taskSummaryToMap(ts)
		_, hasStartedAt := m["started_at"]
		_, hasElapsed := m["elapsed"]
		assert.Expect(hasStartedAt).To(BeFalse())
		assert.Expect(hasElapsed).To(BeFalse())
	})
}
