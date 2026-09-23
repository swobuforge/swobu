package continuity

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

const (
	calibrationEnabledEnv  = "SWOBU_CONTINUITY_CALIBRATION"
	calibrationScenarioEnv = "SWOBU_CONTINUITY_CALIBRATION_SCENARIO"
	calibrationResultEnv   = "SWOBU_CONTINUITY_CALIBRATION_RESULT"
	calibrationOutputEnv   = "SWOBU_CONTINUITY_CALIBRATION_OUTPUT"
)

type calibrationSample struct {
	Scenario          string  `json:"scenario"`
	UsedWeightBytes   uint64  `json:"used_weight_bytes"`
	HeapAllocBytes    int64   `json:"heap_alloc_delta_bytes"`
	HeapInuseBytes    int64   `json:"heap_inuse_delta_bytes"`
	RSSBytes          int64   `json:"rss_delta_bytes"`
	HeapAllocToWeight float64 `json:"heap_alloc_to_weight"`
}

type calibrationReport struct {
	Samples             []calibrationSample `json:"samples"`
	P50HeapAllocRatio   float64             `json:"p50_heap_alloc_ratio"`
	P95HeapAllocRatio   float64             `json:"p95_heap_alloc_ratio"`
	WorstHeapAllocRatio float64             `json:"worst_heap_alloc_ratio"`
}

// TestMemoryStoreWeightCalibration measures whether logical retained weight is
// a stable capacity proxy. It deliberately does not require weight to equal
// Go heap bytes or tune estimator constants to one allocator version.
func TestMemoryStoreWeightCalibration(t *testing.T) {
	if os.Getenv(calibrationEnabledEnv) != "1" {
		t.Skip("Continuity heap calibration runs through node-capacity qualification")
	}
	if scenario := os.Getenv(calibrationScenarioEnv); scenario != "" {
		runCalibrationChild(t, scenario)
		return
	}
	scenarios := []string{
		"many_tiny_checkpoints",
		"linear_100", "linear_1000", "linear_10000",
		"budget_64m", "budget_128m", "budget_256m",
		"branch_baseline", "branch_copy", "branch_extension",
		"mixed_realistic",
	}
	samples := make([]calibrationSample, 0, len(scenarios))
	for _, scenario := range scenarios {
		samples = append(samples, runCalibrationSubprocess(t, scenario))
	}
	report := calibrationReport{Samples: samples}
	ratios := make([]float64, 0, len(samples))
	for _, sample := range samples {
		if sample.UsedWeightBytes == 0 || sample.HeapAllocBytes <= 0 || sample.HeapAllocToWeight <= 0 {
			t.Fatalf("calibration %s produced invalid retained measurement: %+v", sample.Scenario, sample)
		}
		ratios = append(ratios, sample.HeapAllocToWeight)
	}
	sort.Float64s(ratios)
	report.P50HeapAllocRatio = percentile(ratios, 0.50)
	report.P95HeapAllocRatio = percentile(ratios, 0.95)
	report.WorstHeapAllocRatio = ratios[len(ratios)-1]
	if report.WorstHeapAllocRatio > report.P50HeapAllocRatio*4 {
		t.Fatalf("retained heap/weight ratio dispersion is unexplained: p50=%.2f worst=%.2f", report.P50HeapAllocRatio, report.WorstHeapAllocRatio)
	}
	assertLinearCalibration(t, samples)
	assertBudgetCalibration(t, samples)
	assertBranchCalibration(t, samples)
	if output := os.Getenv(calibrationOutputEnv); output != "" {
		raw, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(output, append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("Continuity retained HeapAlloc/weight ratio p50=%.2fx p95=%.2fx worst=%.2fx", report.P50HeapAllocRatio, report.P95HeapAllocRatio, report.WorstHeapAllocRatio)
}

func runCalibrationSubprocess(t *testing.T, scenario string) calibrationSample {
	t.Helper()
	resultPath := fmt.Sprintf("%s/%s.json", t.TempDir(), scenario)
	command := exec.Command(os.Args[0], "-test.run=^TestMemoryStoreWeightCalibration$", "-test.count=1")
	command.Env = append(os.Environ(), calibrationScenarioEnv+"="+scenario, calibrationResultEnv+"="+resultPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("calibration subprocess %s: %v\n%s", scenario, err, output)
	}
	raw, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var sample calibrationSample
	if err := json.Unmarshal(raw, &sample); err != nil {
		t.Fatal(err)
	}
	return sample
}

func runCalibrationChild(t *testing.T, scenario string) {
	t.Helper()
	forceCalibrationGC()
	before := readCalibrationMemory()
	store := buildCalibrationScenario(t, scenario)
	forceCalibrationGC()
	after := readCalibrationMemory()
	store.mu.RLock()
	weight := store.usedWeight
	store.mu.RUnlock()
	sample := calibrationSample{
		Scenario: scenario, UsedWeightBytes: weight,
		HeapAllocBytes: int64(after.heapAlloc) - int64(before.heapAlloc),
		HeapInuseBytes: int64(after.heapInuse) - int64(before.heapInuse),
		RSSBytes:       int64(after.rss) - int64(before.rss),
	}
	if weight > 0 {
		sample.HeapAllocToWeight = float64(sample.HeapAllocBytes) / float64(weight)
	}
	raw, err := json.Marshal(sample)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv(calibrationResultEnv), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime.KeepAlive(store)
}

type calibrationMemory struct {
	heapAlloc uint64
	heapInuse uint64
	rss       uint64
}

func forceCalibrationGC() {
	runtime.GC()
	runtime.GC()
}

func readCalibrationMemory() calibrationMemory {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return calibrationMemory{heapAlloc: stats.HeapAlloc, heapInuse: stats.HeapInuse, rss: processRSS()}
}

func processRSS() uint64 {
	raw, _ := os.ReadFile("/proc/self/status")
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "VmRSS:" {
			value, _ := strconv.ParseUint(fields[1], 10, 64)
			return value * 1024
		}
	}
	return 0
}

func buildCalibrationScenario(t *testing.T, scenario string) *memoryStore {
	t.Helper()
	switch scenario {
	case "many_tiny_checkpoints":
		return calibrationRoots(t, 8000, func(index int) calibrationItems {
			return calibrationItems{request: []canonical.CanonicalItem{message(t, fmt.Sprintf("tiny-%06d", index))}}
		})
	case "linear_100", "linear_1000", "linear_10000":
		turns, _ := strconv.Atoi(strings.TrimPrefix(scenario, "linear_"))
		return calibrationLinear(t, turns)
	case "budget_64m", "budget_128m", "budget_256m":
		megabytes, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(scenario, "budget_"), "m"))
		return calibrationBudget(t, uint64(megabytes)<<20)
	case "branch_baseline":
		return calibrationBranch(t, 0)
	case "branch_copy":
		return calibrationBranch(t, 1)
	case "branch_extension":
		return calibrationBranch(t, 2)
	case "mixed_realistic":
		return calibrationRoots(t, 256, func(index int) calibrationItems {
			size := 256 + index%16*128
			if index%17 == 0 {
				size = 32 << 10
			}
			return calibrationItems{request: []canonical.CanonicalItem{message(t, strings.Repeat("m", size))}, response: responseWithText(nil, canonical.SwobuResponseID(fmt.Sprintf("unused_%d", index)), strings.Repeat("a", size/2)).Items()}
		})
	default:
		t.Fatalf("unknown calibration scenario %q", scenario)
		return nil
	}
}

type calibrationItems struct {
	request  []canonical.CanonicalItem
	response []canonical.CanonicalItem
}

func calibrationRoots(t *testing.T, count int, items func(int) calibrationItems) *memoryStore {
	t.Helper()
	store := newMemoryStore(1 << 60)
	for index := 0; index < count; index++ {
		payload := items(index)
		id := canonical.SwobuResponseID(fmt.Sprintf("cal_%d", index))
		record := storeRecord(id, nil)
		record.Request = canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: payload.request})
		if payload.response != nil {
			response, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: id}, "m", payload.response, canonical.Completed("stop"), canonical.NewUnknownTokenUsage())
			if err != nil {
				t.Fatal(err)
			}
			record.Response = response
		}
		mustPut(t, store, "calibration", Commit{Checkpoint: record})
	}
	return store
}

func calibrationLinear(t *testing.T, turns int) *memoryStore {
	t.Helper()
	store := newMemoryStore(1 << 60)
	chain := &historyChain{}
	var tail *historySegment
	for turn := 0; turn < turns; turn++ {
		id := canonical.SwobuResponseID(fmt.Sprintf("linear_%d", turn))
		input := message(t, fmt.Sprintf("u-%06d", turn))
		tail = appendCalibrationCheckpoint(t, store, chain, tail, id, []canonical.CanonicalItem{input})
	}
	return store
}

func calibrationBudget(t *testing.T, targetWeight uint64) *memoryStore {
	t.Helper()
	store := newMemoryStore(1 << 60)
	for index := 0; store.usedWeight < targetWeight; index++ {
		// Each checkpoint owns a distinct backing string so retained heap and
		// logical weight scale together instead of sharing one fixture payload.
		payload := fmt.Sprintf("%08d:%s", index, strings.Repeat("b", 256<<10))
		id := canonical.SwobuResponseID(fmt.Sprintf("budget_%d", index))
		record := storeRecord(id, nil)
		record.Request = canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{message(t, payload)},
		})
		mustPut(t, store, "calibration", Commit{Checkpoint: record})
	}
	return store
}

func calibrationBranch(t *testing.T, phase int) *memoryStore {
	t.Helper()
	store := calibrationLinear(t, 1000)
	if phase == 0 {
		return store
	}
	predecessor := store.records[workspaceRecordID{workspaceSlug: "calibration", id: "linear_499"}]
	if predecessor == nil {
		t.Fatal("branch predecessor is missing")
	}
	chain := &historyChain{}
	prefix := materializeItems(predecessor.afterTail)
	for index := range prefix {
		prefix[index] = prefix[index].Clone()
	}
	tail := appendCalibrationCheckpoint(t, store, chain, nil, "branch_0", append(prefix, message(t, "branch")))
	if phase == 1 {
		return store
	}
	appendCalibrationCheckpoint(t, store, chain, tail, "branch_1", []canonical.CanonicalItem{message(t, "next")})
	return store
}

// appendCalibrationCheckpoint constructs the same retained storage shape as
// Put after request validation. It avoids materializing every historical
// request during fixture setup, which would measure setup's quadratic clone
// traffic instead of the store's live append-only representation.
func appendCalibrationCheckpoint(t *testing.T, store *memoryStore, chain *historyChain, parent *historySegment, id canonical.SwobuResponseID, requestItems []canonical.CanonicalItem) *historySegment {
	t.Helper()
	record := storeRecord(id, nil)
	record.Response = responseWithText(nil, id, fmt.Sprintf("a-%s", id))
	responseItems := record.Response.Items()
	requestWeights, err := canonicalItemWeights(requestItems)
	if err != nil {
		t.Fatal(err)
	}
	responseWeight, err := canonicalItemsWeight(responseItems)
	if err != nil {
		t.Fatal(err)
	}
	preludeWeight, err := canonicalItemsWeight(nil)
	if err != nil {
		t.Fatal(err)
	}
	localWeight, err := checkpointLocalWeight("calibration", record, preludeWeight, responseWeight)
	if err != nil {
		t.Fatal(err)
	}
	requestTail := appendSegment(parent, requestItems)
	afterTail := appendSegment(requestTail, responseItems)
	addedHistoryWeight := historySegmentRetainedWeight(requestWeights)
	if len(responseItems) > 0 {
		addedHistoryWeight += historySegmentWeight + responseWeight
	}
	key := workspaceRecordID{workspaceSlug: "calibration", id: id}
	stored := &storedCheckpoint{checkpoint: record, requestTail: requestTail, afterTail: afterTail, chain: chain, localWeight: localWeight}
	stored.order = store.order.PushBack(orderedRecord{key: key})
	store.records[key] = stored
	chain.head = id
	chain.anchors++
	chain.historyWeight += addedHistoryWeight
	store.usedWeight += localWeight + addedHistoryWeight
	return afterTail
}

func percentile(sorted []float64, quantile float64) float64 {
	index := int(float64(len(sorted)-1) * quantile)
	return sorted[index]
}

func sampleByName(t *testing.T, samples []calibrationSample, name string) calibrationSample {
	t.Helper()
	for _, sample := range samples {
		if sample.Scenario == name {
			return sample
		}
	}
	t.Fatalf("missing calibration sample %q", name)
	return calibrationSample{}
}

func assertLinearCalibration(t *testing.T, samples []calibrationSample) {
	t.Helper()
	previous := sampleByName(t, samples, "linear_100")
	for _, name := range []string{"linear_1000", "linear_10000"} {
		current := sampleByName(t, samples, name)
		if current.UsedWeightBytes <= previous.UsedWeightBytes || current.HeapAllocBytes <= previous.HeapAllocBytes {
			t.Fatalf("linear calibration is not monotonic: previous=%+v current=%+v", previous, current)
		}
		previous = current
	}
	thousand := sampleByName(t, samples, "linear_1000")
	tenThousand := sampleByName(t, samples, "linear_10000")
	if tenThousand.HeapAllocToWeight > thousand.HeapAllocToWeight*2 {
		t.Fatalf("linear retained heap grows superlinearly: 1000=%.2fx 10000=%.2fx", thousand.HeapAllocToWeight, tenThousand.HeapAllocToWeight)
	}
}

func assertBudgetCalibration(t *testing.T, samples []calibrationSample) {
	t.Helper()
	previous := sampleByName(t, samples, "budget_64m")
	for _, name := range []string{"budget_128m", "budget_256m"} {
		current := sampleByName(t, samples, name)
		if current.UsedWeightBytes <= previous.UsedWeightBytes || current.HeapAllocBytes <= previous.HeapAllocBytes {
			t.Fatalf("budget calibration is not monotonic: previous=%+v current=%+v", previous, current)
		}
		if current.HeapAllocToWeight > previous.HeapAllocToWeight*1.5 {
			t.Fatalf("budget calibration grows disproportionately: previous=%+v current=%+v", previous, current)
		}
		previous = current
	}
}

func assertBranchCalibration(t *testing.T, samples []calibrationSample) {
	t.Helper()
	baseline := sampleByName(t, samples, "branch_baseline")
	branch := sampleByName(t, samples, "branch_copy")
	extension := sampleByName(t, samples, "branch_extension")
	copyWeight := branch.UsedWeightBytes - baseline.UsedWeightBytes
	extensionWeight := extension.UsedWeightBytes - branch.UsedWeightBytes
	copyHeap := branch.HeapAllocBytes - baseline.HeapAllocBytes
	extensionHeap := extension.HeapAllocBytes - branch.HeapAllocBytes
	if extensionHeap < 0 {
		extensionHeap = -extensionHeap
	}
	if copyHeap <= 0 || copyHeap <= extensionHeap*4 || copyWeight <= extensionWeight*4 {
		t.Fatalf("branch calibration does not show one-time prefix copy: baseline=%+v branch=%+v extension=%+v", baseline, branch, extension)
	}
}
