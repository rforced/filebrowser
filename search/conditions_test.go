package search

import "testing"

func TestParseSearchTypes(t *testing.T) {
	cases := []struct {
		query   string
		match   []string
		noMatch []string
	}{
		{
			query:   "type:inputs",
			match:   []string{"/case/inputs.in", "/case/combust.in"},
			noMatch: []string{"/case/thermo.out", "/case/inputs.in.bak"},
		},
		{
			query:   "type:outputs",
			match:   []string{"/case/thermo.out", "/case/regions_flow.out"},
			noMatch: []string{"/case/inputs.in", "/case/outputs_restart1"},
		},
		{
			query:   "type:logs",
			match:   []string{"/case/converge.log"},
			noMatch: []string{"/case/converge.log.1"},
		},
		{
			query:   "type:restarts",
			match:   []string{"/case/restart000100.rst"},
			noMatch: []string{"/case/post000100.h5"},
		},
		{
			query: "type:fields",
			match: []string{
				"/case/outputs_original/output/post000014_-3.59945e+02.h5",
				"/case/outputs_original/paraview_catalyst/slice1_STREAM_00_000009.cgns",
			},
			// The container alone means nothing: restarts, mapped initial
			// conditions and lookup tables are all HDF5 too.
			noMatch: []string{
				"/case/map.h5",
				"/case/sl_table.h5",
				"/case/outputs_original/stream0/map_-1.201942e+02.h5",
				"/case/outputs_original/restart0074.rst",
			},
		},
		{
			query:   "type:pdf",
			match:   []string{"/case/report.pdf"},
			noMatch: []string{"/case/inputs.in"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			opts := parseSearch(tc.query)
			if len(opts.Conditions) != 1 {
				t.Fatalf("got %d conditions, want 1", len(opts.Conditions))
			}
			if len(opts.Terms) != 0 {
				t.Fatalf("got terms %v, want none", opts.Terms)
			}

			for _, path := range tc.match {
				if !opts.Conditions[0](path) {
					t.Errorf("%q should match %s", path, tc.query)
				}
			}
			for _, path := range tc.noMatch {
				if opts.Conditions[0](path) {
					t.Errorf("%q should not match %s", path, tc.query)
				}
			}
		})
	}
}

func TestParseSearchTypeSingularAliases(t *testing.T) {
	for query, path := range map[string]string{
		"type:input":   "/case/inputs.in",
		"type:output":  "/case/thermo.out",
		"type:log":     "/case/converge.log",
		"type:restart": "/case/restart000100.rst",
	} {
		opts := parseSearch(query)
		if len(opts.Conditions) != 1 || !opts.Conditions[0](path) {
			t.Errorf("%q should match %s", path, query)
		}
	}
}
