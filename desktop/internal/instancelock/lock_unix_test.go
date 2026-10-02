//go:build !windows

package instancelock

import "testing"

func TestInstanceLockAllowsOnlyOneHolder(t *testing.T) {
	dir := t.TempDir()
	releaseFirst, acquired, err := Acquire(dir)
	if err != nil || !acquired {
		t.Fatalf("first lock: acquired=%v err=%v", acquired, err)
	}
	releaseSecond, acquired, err := Acquire(dir)
	if err != nil || acquired {
		t.Fatalf("second lock: acquired=%v err=%v", acquired, err)
	}
	releaseSecond()
	releaseFirst()

	releaseThird, acquired, err := Acquire(dir)
	if err != nil || !acquired {
		t.Fatalf("lock after release: acquired=%v err=%v", acquired, err)
	}
	releaseThird()
}
