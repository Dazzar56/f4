package main

import "testing"

// The two cases in the f4probe-results.zip that the old verdict got wrong,
// plus the healthy shapes, pinned so the rules cannot regress.
func TestF4MatrixVerdict(t *testing.T) {
	// One good oracle pass and one safely-rejected dynamic frame: the mode
	// worked. The old rule saw "nothing stamped" anywhere and failed it.
	probeLog := "REFLOW: F4_WIN_REFLOW=probe\n" +
		"REFLOW_ORACLE: wide frame: 12 content rows, delimited=true; narrow frame: delimited=true; viewport 100x28\n" +
		"REFLOW_ORACLE: 6/28 repaint rows aligned with history+viewport; 2 safe boundaries, 0 where hint and oracle disagree\n" +
		"REFLOW_ORACLE: wide frame: 6 content rows, delimited=true; narrow frame: delimited=true; viewport 80x22\n" +
		"REFLOW_ORACLE: mismatch: no consecutive repaint rows occur in the local history+viewport journal; nothing stamped\n"

	// off mode: the banner drain returned zero, but the run is healthy -- the
	// STARTUP line and the screen bytes prove it. The old rule failed it on
	// startup==0 alone.
	offLog := "REFLOW: F4_WIN_REFLOW=off\n=== F4 STARTUP [(devel)] PID:2704 ===\n"

	cases := []struct {
		name         string
		obs          f4MatrixObservations
		wantVerdict  string
		wantPasses   int
		wantStamped  int
		wantRejected int
	}{
		{
			name: "probe: one stamped, one safely rejected -> complete",
			obs: f4MatrixObservations{
				mode: "probe", startupLen: 13498, screenLen: 93887,
				debugLog: probeLog, resizeOK: true,
			},
			wantVerdict: "complete", wantPasses: 2, wantStamped: 1, wantRejected: 1,
		},
		{
			name: "off: raced startup drain but STARTUP logged -> complete",
			obs: f4MatrixObservations{
				mode: "off", startupLen: 0, screenLen: 114780,
				debugLog: offLog, resizeOK: true,
			},
			wantVerdict: "complete",
		},
		{
			name: "oracle: every pass rejected -> incomplete",
			obs: f4MatrixObservations{
				mode: "oracle", startupLen: 100, screenLen: 100, resizeOK: true,
				debugLog: "REFLOW: F4_WIN_REFLOW=oracle\n" +
					"REFLOW_ORACLE: wide frame: 6 content rows\nnothing stamped\n",
			},
			wantVerdict: "incomplete", wantPasses: 1, wantStamped: 0, wantRejected: 1,
		},
		{
			name: "any mode: nothing started at all -> incomplete",
			obs: f4MatrixObservations{
				mode: "hint", startupLen: 0, screenLen: 0,
				debugLog: "", resizeOK: true,
			},
			wantVerdict: "incomplete",
		},
		{
			name: "hint: mode line missing -> incomplete",
			obs: f4MatrixObservations{
				mode: "hint", startupLen: 100, screenLen: 100, resizeOK: true,
				debugLog: "=== F4 STARTUP ===\n",
			},
			wantVerdict: "incomplete",
		},
		{
			name: "oracle: healthy resize failure -> incomplete",
			obs: f4MatrixObservations{
				mode: "oracle", startupLen: 100, screenLen: 100, resizeOK: false,
				debugLog: "REFLOW: F4_WIN_REFLOW=oracle\n" +
					"REFLOW_ORACLE: wide frame: 10\n2 safe boundaries\n",
			},
			wantVerdict: "incomplete", wantPasses: 1, wantStamped: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			verdict, passes, stamped, rejected := f4MatrixVerdict(tc.obs)
			if verdict != tc.wantVerdict {
				t.Errorf("verdict = %q, want %q", verdict, tc.wantVerdict)
			}
			if passes != tc.wantPasses {
				t.Errorf("passes = %d, want %d", passes, tc.wantPasses)
			}
			if stamped != tc.wantStamped {
				t.Errorf("stamped = %d, want %d", stamped, tc.wantStamped)
			}
			if rejected != tc.wantRejected {
				t.Errorf("rejected = %d, want %d", rejected, tc.wantRejected)
			}
		})
	}
}
