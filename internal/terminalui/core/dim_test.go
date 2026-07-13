package core

import "testing"

func TestMinMaxValid(t *testing.T) {
	t.Parallel()

	dim := MinMax(5, Fixed(20))
	if dim.Mode != DimMinMax {
		t.Fatalf("mode = %v, want DimMinMax", dim.Mode)
	}
	if dim.Min != 5 {
		t.Fatalf("min = %d, want 5", dim.Min)
	}
	if dim.Max != 20 {
		t.Fatalf("max = %d, want 20", dim.Max)
	}

	dimFill := MinMax(0, Fill(1))
	if dimFill.Mode != DimMinMax {
		t.Fatalf("mode = %v, want DimMinMax", dimFill.Mode)
	}
	if dimFill.Min != 0 {
		t.Fatalf("min = %d, want 0", dimFill.Min)
	}
	if dimFill.Max != 0 {
		t.Fatalf("max = %d, want 0", dimFill.Max)
	}
	if dimFill.Weight != 1 {
		t.Fatalf("weight = %d, want 1", dimFill.Weight)
	}
}

func TestMinMaxRejectsFitMax(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("MinMax with Fit() max should panic")
		}
	}()
	_ = MinMax(5, Fit())
}
