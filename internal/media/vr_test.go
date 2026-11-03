package media

import "testing"

func TestDetectShape(t *testing.T) {
	cases := []struct {
		name string
		w, h int
		proj string
		st   string
		fov  int
	}{
		{"StudioVR.Jane.Doe.Morning.Light.Oculus,.Quest.2..4K..mp4", 3840, 1920, "equirect180", "sbs", 180},
		{"SampleVRCasting.E068.Jane.Doe.VR180.1920p.MP4-GRP.mp4", 3840, 1920, "equirect180", "sbs", 180},
		{"DarkStudioVR.Night_Session.2700p.mp4", 5400, 2700, "equirect180", "sbs", 180},
		{"2018.02.02.Virtual.Real.Studio.Holiday.Special.2160p.mp4", 4320, 2160, "equirect180", "sbs", 180},
		{"scene_MKX200_LR.mp4", 5760, 2880, "fisheye", "sbs", 200},
		{"tour_360_TB.mp4", 4096, 4096, "equirect360", "tb", 360},
		{"Studio.20.10.15.Jane.Doe.Movie.Night.1080p.MP4-GRP.mp4", 1920, 1080, "flat", "mono", 0},
		{"Morning Coffee with Jane - STUDIO.mp4", 1920, 1080, "flat", "mono", 0},
	}
	for _, c := range cases {
		sh := DetectShape(c.name, c.w, c.h, "", "")
		if sh.Projection != c.proj || sh.Stereo != c.st || sh.FOV != c.fov {
			t.Errorf("%s: got %+v, want %s %s %d", c.name, sh, c.proj, c.st, c.fov)
		}
	}
}

func TestHashDistance(t *testing.T) {
	if d := HashDistance("ffffffffffffffff", "fffffffffffffffe"); d != 1 {
		t.Fatalf("distance %d", d)
	}
}
