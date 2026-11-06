package files

import "testing"

func TestReadWindowBounds(t *testing.T) {
	for _, tc := range []struct {
		size, offset int64
		length       *int64
		want         int64
		bad          bool
	}{
		{10, 0, nil, 10, false}, {10, 4, nil, 6, false}, {0, 0, nil, 0, false}, {10, 10, nil, 0, false},
		{10, -1, nil, 0, true}, {10, 11, nil, 0, true}, {-1, 0, nil, 0, true},
	} {
		got, err := ReadWindow(tc.size, tc.offset, tc.length)
		if (err != nil) != tc.bad || (!tc.bad && got != tc.want) {
			t.Fatalf("%+v: %d %v", tc, got, err)
		}
	}
	for _, n := range []int64{-1, 7, 1000} {
		if _, err := ReadWindow(10, 4, &n); err == nil {
			t.Fatal("oversized/negative window accepted")
		}
	}
	zero := int64(0)
	if n, err := ReadWindow(10, 4, &zero); err != nil || n != 0 {
		t.Fatal("explicit empty window")
	}
}
