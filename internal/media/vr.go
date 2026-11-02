package media

import (
	"regexp"
	"strings"
)

// VR studios and words in file names ("BaDoinkVR", "CzechVRCasting",
// "Oculus", "VR180"), compared with the name squashed to lower-case letters
// and digits.
var vrWords = []string{
	"vr180", "vr360", "oculus", "gearvr", "psvr", "quest2", "quest3", "virtualrealporn", "virtualreal", "sexlikereal", "virtualtaboo",
	"badoinkvr", "czechvr", "wankzvr", "vrbangers", "darkroomvr", "vrhush", "povr", "milfvr", "realjamvr", "vrconk", "stasyqvr",
	"vrcosplayx", "18vr", "vrlatina", "naughtyamericavr", "vrporn", "lethalhardcorevr", "kinkvr", "tmwvrnet", "vrallure", "vrspy",
	"swallowbay", "vrsolos", "babevr", "vrintimacy", "only3x", "holivr", "dezyred", "realitylovers", "virtualpee", "vredging",
	"perfectgirlfriend", "pornbcn", "vrfirsttimer", "vrmansion", "squeezevr", "baberoticavr", "tadpolexxx", "vrxdb",
}

var tokenRe = regexp.MustCompile(`[a-z0-9]+`)

// DetectShape works out a video's VR shape from its name, its frame size
// and spherical metadata. Flat videos come back as Shape{Projection: "flat"}.
func DetectShape(name string, w, h int, spherical, stereo3d string) Shape {
	lower := strings.ToLower(name)
	toks := tokenRe.FindAllString(lower, -1)
	has := map[string]bool{}
	for _, t := range toks {
		has[t] = true
	}
	squashed := strings.Join(toks, "")

	vr := spherical != ""
	for _, t := range toks {
		if t == "vr" || (len(t) >= 4 && (strings.HasPrefix(t, "vr") || strings.HasSuffix(t, "vr")) && !strings.HasPrefix(t, "vrai")) {
			vr = true
		}
	}
	for _, w := range vrWords {
		if strings.Contains(squashed, w) {
			vr = true
		}
	}

	sh := Shape{Projection: "flat", Stereo: "mono"}
	// the lens: fisheye names win over everything
	switch {
	case has["mkx200"] || strings.Contains(squashed, "mkx200"):
		return Shape{"fisheye", stereoOf(has, w, h, "sbs"), 200}
	case has["mkx220"] || has["vrca220"] || strings.Contains(squashed, "mkx220") || strings.Contains(squashed, "vrca220"):
		return Shape{"fisheye", stereoOf(has, w, h, "sbs"), 220}
	case has["rf52"] || strings.Contains(squashed, "fisheye190") || has["fisheye190"]:
		return Shape{"fisheye", stereoOf(has, w, h, "sbs"), 190}
	case has["fisheye"] || spherical == "fisheye":
		return Shape{"fisheye", stereoOf(has, w, h, "sbs"), 180}
	}
	switch {
	case has["360"] || has["vr360"] || strings.Contains(lower, "_360_") || strings.Contains(squashed, "360x"):
		sh.Projection = "equirect360"
		vr = true
	case has["180"] || has["vr180"] || strings.Contains(lower, "_180_"):
		sh.Projection = "equirect180"
		vr = true
	}
	if !vr {
		return sh
	}
	// stereo from the name, then from metadata, then from the frame shape
	sh.Stereo = stereoOf(has, w, h, "")
	if sh.Stereo == "" {
		switch {
		case strings.Contains(stereo3d, "side"):
			sh.Stereo = "sbs"
		case strings.Contains(stereo3d, "top"):
			sh.Stereo = "tb"
		}
	}
	ratio := 0.0
	if h > 0 {
		ratio = float64(w) / float64(h)
	}
	if sh.Projection == "flat" {
		switch {
		case spherical == "equirectangular" && sh.Stereo != "sbs" && ratio > 1.8 && ratio < 2.2:
			sh.Projection = "equirect360"
		case ratio > 0 && ratio < 1.2 && sh.Stereo != "sbs":
			// square frame, eyes on top of each other: 360°; tall: 180°
			sh.Projection = "equirect360"
			if ratio < 0.7 {
				sh.Projection = "equirect180"
			}
		default:
			sh.Projection = "equirect180"
		}
	}
	if sh.Stereo == "" {
		switch {
		case ratio == 0: // size not known yet: side by side is by far the most common
			sh.Stereo = "sbs"
		case ratio >= 1.8:
			sh.Stereo = "sbs"
			if sh.Projection == "equirect360" && ratio < 2.2 {
				sh.Stereo = "mono"
			}
		case ratio > 0 && ratio <= 1.2:
			sh.Stereo = "tb"
		default:
			sh.Stereo = "mono"
		}
	}
	sh.FOV = 180
	if sh.Projection == "equirect360" {
		sh.FOV = 360
	}
	return sh
}

func stereoOf(has map[string]bool, w, h int, fallback string) string {
	switch {
	case has["lr"] || has["sbs"] || has["3dh"] || has["rl"]:
		return "sbs"
	case has["tb"] || has["ou"] || has["3dv"] || has["bt"] || has["overunder"]:
		return "tb"
	case has["mono"] || has["2d"]:
		return "mono"
	}
	if fallback != "" && h > 0 && float64(w)/float64(h) < 1.3 {
		return "tb"
	}
	return fallback
}
