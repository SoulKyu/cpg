package dropclass

// ResetWarnStateForTest clears the dedup map and resets the logger between tests.
// Must be called at the start of each test that exercises the warn path.
func ResetWarnStateForTest() {
	warnedUnknown.Range(func(k, _ any) bool {
		warnedUnknown.Delete(k)
		return true
	})
}
