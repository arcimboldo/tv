package eztv

import "testing"

func Test_fuzzyPathMatching(t *testing.T) {
	tests := []struct {
		input  []string
		expect bool
	}{
		{[]string{"foo", "foo"}, true},
		{[]string{"foo.avi", "foo.mkv"}, false},
		{[]string{"foo[eztv]", "foo"}, true},
		{[]string{"foo", "foo[eztv]"}, true},
		{[]string{"FOO", "Foo"}, true},
		{[]string{"FoO", "foo[eztv]"}, true},
		{[]string{
			"mr.robot.s03e04.internal.720p.web.x264-bamboozle.mkv",
			"Mr.Robot.S03E04.iNTERNAL.720p.WEB.x264-BAMBOOZLE[eztv].mkv"},
			true},
	}

	for _, test := range tests {
		if fuzzyPathMatching(test.input[0], test.input[1]) != test.expect {
			t.Errorf("testing %q and %q, expected %v, got %v instead", test.input[0], test.input[1], test.expect, fuzzyPathMatching(test.input[0], test.input[1]))
		}
	}

}
