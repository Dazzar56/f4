package main

import "strings"

// f4MatrixObservations is everything read from one f4 mode run that the verdict
// depends on. It is separated from the Windows-only runner so the rules can be
// tested on any platform: the two mistakes this file exists to prevent (a
// false incomplete from a raced startup drain, and a false incomplete from a
// single safely-rejected oracle frame) were both verdict-logic bugs, not
// measurement bugs.
type f4MatrixObservations struct {
	mode       string
	startupLen int
	screenLen  int
	nestedOut  string
	debugLog   string
	logReadErr bool
	resizeOK   bool
}

// f4MatrixVerdict derives the per-mode verdict and the oracle pass counts.
//
// startup is judged from the union of independent signals rather than the
// first drain alone: on a slow machine f4's banner can arrive after a fixed
// drain's quiet window, yet the debug log's "F4 STARTUP" line and the
// accumulated screen bytes still prove the launch. An oracle pass either
// stamps ("safe boundaries") or safely rejects an unstable frame ("nothing
// stamped"); a rejection is correct behaviour, so completeness needs at least
// one pass that stamped, not zero that were rejected.
func f4MatrixVerdict(o f4MatrixObservations) (verdict string, passes, stamped, rejected int) {
	d := o.debugLog
	modeConfirmed := strings.Contains(d, "REFLOW: F4_WIN_REFLOW="+o.mode)
	passes = strings.Count(d, "REFLOW_ORACLE: wide frame:")
	stamped = strings.Count(d, "safe boundaries")
	rejected = strings.Count(d, "nothing stamped")
	oracleCompleted := stamped > 0
	started := o.startupLen > 0 || o.screenLen > 0 || strings.Contains(d, "F4 STARTUP")

	needsOracle := o.mode == "oracle" || o.mode == "probe"
	verdict = "complete"
	if !started || o.logReadErr || !modeConfirmed || !o.resizeOK ||
		(needsOracle && !oracleCompleted) {
		verdict = "incomplete"
	}
	return verdict, passes, stamped, rejected
}

func f4MatrixModeConfirmed(mode, debugLog string) bool {
	return strings.Contains(debugLog, "REFLOW: F4_WIN_REFLOW="+mode)
}

func f4MatrixOracleSeen(debugLog string) bool {
	return strings.Contains(debugLog, "REFLOW_ORACLE:")
}

func f4MatrixSyncSeen(debugLog string) bool {
	return strings.Contains(debugLog, "ANSI_PARSER: Excising background Windows CD sync")
}

func f4MatrixNestedSeen(nestedOut, debugLog string) bool {
	return strings.Contains(nestedOut, "F4PROBE_NESTED_ENTER_OK") ||
		strings.Contains(debugLog, "F4PROBE_NESTED_ENTER_OK")
}
