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
		{"BaDoinkVR.Lola.Myluv.On.Her.Knees.Begging.Oculus,.Quest.2..4K..mp4", 3840, 1920, "equirect180", "sbs", 180},
		{"CzechVRCasting.E068.Lola.Myluv.XXX.VR180.1920p.MP4-GUSH.mp4", 3840, 1920, "equirect180", "sbs", 180},
		{"DarkRoomVR.Fuck_Your_Problems.2700p.mp4", 5400, 2700, "equirect180", "sbs", 180},
		{"2018.02.02.Virtual.Real.Porn.Superbowl.Halftime.2160p.mp4", 4320, 2160, "equirect180", "sbs", 180},
		{"scene_MKX200_LR.mp4", 5760, 2880, "fisheye", "sbs", 200},
		{"tour_360_TB.mp4", 4096, 4096, "equirect360", "tb", 360},
		{"HollyRandall.20.10.15.Lola.Myluv.Movie.Night.XXX.1080p.MP4-WRB.mp4", 1920, 1080, "flat", "mono", 0},
		{"Hot Blonde Lola Myluv Has Morning Sex with Boyfriend - WHITEBOXXX.mp4", 1920, 1080, "flat", "mono", 0},
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
