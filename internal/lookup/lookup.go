// Package lookup finds what a scene is on StashDB (or another stash-box)
// and ThePornDB: by the file's fingerprints, or by searching its name.
package lookup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// StashDB is the default stash-box.
const StashDB = "https://stashdb.org/graphql"

// Studio as a lookup returns it.
type Studio struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	URL    string `json:"url"`
	Image  string `json:"image"`
	Parent string `json:"parent"`
}

// Performer as a lookup returns it.
type Performer struct {
	Source         string   `json:"source"`
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	As             string   `json:"as,omitempty"` // credited under this name
	Disambiguation string   `json:"disambiguation"`
	Aliases        []string `json:"aliases"`
	Gender         string   `json:"gender"`
	Birthdate      string   `json:"birthdate"`
	DeathDate      string   `json:"death_date"`
	Country        string   `json:"country"`
	Ethnicity      string   `json:"ethnicity"`
	EyeColor       string   `json:"eye_color"`
	HairColor      string   `json:"hair_color"`
	HeightCM       int      `json:"height_cm"`
	Measurements   string   `json:"measurements"`
	FakeTits       string   `json:"fake_tits"`
	Career         string   `json:"career"`
	Tattoos        string   `json:"tattoos"`
	Piercings      string   `json:"piercings"`
	URLs           []string `json:"urls"`
	Image          string   `json:"image"`
}

// Scene as a lookup returns it.
type Scene struct {
	Source     string      `json:"source"` // stashdb | tpdb
	Endpoint   string      `json:"endpoint"`
	ID         string      `json:"id"`
	Title      string      `json:"title"`
	Code       string      `json:"code"`
	Details    string      `json:"details"`
	Director   string      `json:"director"`
	Date       string      `json:"date"`
	Duration   int         `json:"duration"`
	URLs       []string    `json:"urls"`
	Image      string      `json:"image"`
	Studio     *Studio     `json:"studio"`
	Performers []Performer `json:"performers"`
	Tags       []string    `json:"tags"`
	Matched    string      `json:"matched,omitempty"` // which fingerprint matched (OSHASH, PHASH)
}

var client = &http.Client{Timeout: 45 * time.Second}

// ---------- stash-box ----------

// StashBox talks to a stash-box GraphQL API.
type StashBox struct {
	Endpoint string
	Key      string
	UA       string
}

const sceneFields = `fragment I on Image { url width height }
fragment P on Performer { id name disambiguation aliases gender urls { url } images { ...I } birth_date death_date ethnicity country
  eye_color hair_color height measurements { band_size cup_size waist hip } breast_type career_start_year career_end_year
  tattoos { location description } piercings { location description } }
fragment S on Scene { id title code details director duration date urls { url } images { ...I }
  studio { id name urls { url } images { ...I } parent { name } } tags { name } performers { as performer { ...P } }
  fingerprints { algorithm hash } }`

type sbImage struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type sbURL struct {
	URL string `json:"url"`
}

type sbMod struct {
	Location    string `json:"location"`
	Description string `json:"description"`
}

type sbPerformer struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Disambiguation string    `json:"disambiguation"`
	Aliases        []string  `json:"aliases"`
	Gender         string    `json:"gender"`
	URLs           []sbURL   `json:"urls"`
	Images         []sbImage `json:"images"`
	BirthDate      string    `json:"birth_date"`
	DeathDate      string    `json:"death_date"`
	Ethnicity      string    `json:"ethnicity"`
	Country        string    `json:"country"`
	EyeColor       string    `json:"eye_color"`
	HairColor      string    `json:"hair_color"`
	Height         int       `json:"height"`
	Measurements   *struct {
		Band int    `json:"band_size"`
		Cup  string `json:"cup_size"`
		W    int    `json:"waist"`
		H    int    `json:"hip"`
	} `json:"measurements"`
	BreastType string  `json:"breast_type"`
	Start      int     `json:"career_start_year"`
	End        int     `json:"career_end_year"`
	Tattoos    []sbMod `json:"tattoos"`
	Piercings  []sbMod `json:"piercings"`
}

type sbScene struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Code     string    `json:"code"`
	Details  string    `json:"details"`
	Director string    `json:"director"`
	Duration int       `json:"duration"`
	Date     string    `json:"date"`
	URLs     []sbURL   `json:"urls"`
	Images   []sbImage `json:"images"`
	Studio   *struct {
		ID     string    `json:"id"`
		Name   string    `json:"name"`
		URLs   []sbURL   `json:"urls"`
		Images []sbImage `json:"images"`
		Parent *struct {
			Name string `json:"name"`
		} `json:"parent"`
	} `json:"studio"`
	Tags []struct {
		Name string `json:"name"`
	} `json:"tags"`
	Performers []struct {
		As        string      `json:"as"`
		Performer sbPerformer `json:"performer"`
	} `json:"performers"`
	Fingerprints []struct {
		Algorithm string `json:"algorithm"`
		Hash      string `json:"hash"`
	} `json:"fingerprints"`
}

func biggest(imgs []sbImage) string {
	best, area := "", 0
	for _, im := range imgs {
		if a := im.Width * im.Height; best == "" || a > area {
			best, area = im.URL, a
		}
	}
	return best
}

func urls(us []sbURL) []string {
	out := []string{}
	for _, u := range us {
		out = append(out, u.URL)
	}
	return out
}

func mods(ms []sbMod) string {
	var parts []string
	for _, m := range ms {
		parts = append(parts, strings.TrimSpace(strings.Trim(m.Location+" "+m.Description, " ")))
	}
	return strings.Join(parts, "; ")
}

func (p sbPerformer) toPerformer(endpoint string) Performer {
	out := Performer{Source: "stashdb", ID: p.ID, Name: p.Name, Disambiguation: p.Disambiguation, Aliases: p.Aliases,
		Gender: strings.ToLower(p.Gender), Birthdate: p.BirthDate, DeathDate: p.DeathDate, Country: p.Country,
		Ethnicity: strings.ToLower(p.Ethnicity), EyeColor: strings.ToLower(p.EyeColor), HairColor: strings.ToLower(p.HairColor),
		HeightCM: p.Height, URLs: urls(p.URLs), Image: biggest(p.Images), Tattoos: mods(p.Tattoos), Piercings: mods(p.Piercings)}
	if m := p.Measurements; m != nil && (m.Band > 0 || m.Cup != "" || m.W > 0 || m.H > 0) {
		out.Measurements = fmt.Sprintf("%d%s-%d-%d", m.Band, m.Cup, m.W, m.H)
	}
	switch strings.ToUpper(p.BreastType) {
	case "FAKE", "AUGMENTED":
		out.FakeTits = "Augmented"
	case "NATURAL":
		out.FakeTits = "Natural"
	}
	if p.Start > 0 {
		out.Career = fmt.Sprint(p.Start) + " -"
		if p.End > 0 {
			out.Career += fmt.Sprint(" ", p.End)
		}
	}
	return out
}

func (s sbScene) toScene(endpoint string) Scene {
	out := Scene{Source: "stashdb", Endpoint: endpoint, ID: s.ID, Title: s.Title, Code: s.Code, Details: s.Details, Director: s.Director,
		Date: s.Date, Duration: s.Duration, URLs: urls(s.URLs), Image: biggest(s.Images), Tags: []string{}, Performers: []Performer{}}
	if s.Studio != nil {
		out.Studio = &Studio{ID: s.Studio.ID, Name: s.Studio.Name, Image: biggest(s.Studio.Images)}
		if len(s.Studio.URLs) > 0 {
			out.Studio.URL = s.Studio.URLs[0].URL
		}
		if s.Studio.Parent != nil {
			out.Studio.Parent = s.Studio.Parent.Name
		}
	}
	for _, t := range s.Tags {
		out.Tags = append(out.Tags, t.Name)
	}
	for _, p := range s.Performers {
		pp := p.Performer.toPerformer(endpoint)
		pp.As = p.As
		out.Performers = append(out.Performers, pp)
	}
	return out
}

func (b *StashBox) query(ctx context.Context, q string, vars map[string]any, out any) error {
	if b.Key == "" {
		return errors.New("no StashDB key — add yours under Settings → Lookups")
	}
	ep := b.Endpoint
	if ep == "" {
		ep = StashDB
	}
	body, _ := json.Marshal(map[string]any{"query": q, "variables": vars})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ep, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ApiKey", b.Key)
	req.Header.Set("User-Agent", b.UA)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return errors.New("StashDB did not accept the key")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("StashDB answered HTTP %d", resp.StatusCode)
	}
	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return err
	}
	if len(env.Errors) > 0 {
		return errors.New("StashDB: " + env.Errors[0].Message)
	}
	return json.Unmarshal(env.Data, out)
}

// ByFingerprints finds scenes whose fingerprints match the file's.
func (b *StashBox) ByFingerprints(ctx context.Context, oshash, phash string) ([]Scene, error) {
	var fps []map[string]string
	if oshash != "" {
		fps = append(fps, map[string]string{"hash": oshash, "algorithm": "OSHASH"})
	}
	if phash != "" {
		fps = append(fps, map[string]string{"hash": phash, "algorithm": "PHASH"})
	}
	if len(fps) == 0 {
		return nil, errors.New("the file has no fingerprint yet")
	}
	var out struct {
		Res [][]sbScene `json:"findScenesBySceneFingerprints"`
	}
	if err := b.query(ctx, sceneFields+` query($fp: [[FingerprintQueryInput!]!]!) { findScenesBySceneFingerprints(fingerprints: $fp) { ...S } }`,
		map[string]any{"fp": [][]map[string]string{fps}}, &out); err != nil {
		return nil, err
	}
	res := []Scene{}
	seen := map[string]bool{}
	for _, group := range out.Res {
		for _, s := range group {
			if seen[s.ID] {
				continue
			}
			seen[s.ID] = true
			sc := s.toScene(b.Endpoint)
			for _, f := range s.Fingerprints {
				if (f.Algorithm == "OSHASH" && f.Hash == oshash) || (f.Algorithm == "PHASH" && f.Hash == phash) {
					sc.Matched = f.Algorithm
					break
				}
			}
			if sc.Matched == "" {
				sc.Matched = "PHASH"
			}
			res = append(res, sc)
		}
	}
	return res, nil
}

// Search finds scenes by words (a title, a file name).
func (b *StashBox) Search(ctx context.Context, term string) ([]Scene, error) {
	var out struct {
		Res []sbScene `json:"searchScene"`
	}
	if err := b.query(ctx, sceneFields+` query($t: String!) { searchScene(term: $t) { ...S } }`, map[string]any{"t": term}, &out); err != nil {
		return nil, err
	}
	res := []Scene{}
	for _, s := range out.Res {
		res = append(res, s.toScene(b.Endpoint))
	}
	return res, nil
}

// SearchPerformer finds performers by name.
func (b *StashBox) SearchPerformer(ctx context.Context, term string) ([]Performer, error) {
	var out struct {
		Res []sbPerformer `json:"searchPerformer"`
	}
	if err := b.query(ctx, `fragment I on Image { url width height }
fragment P on Performer { id name disambiguation aliases gender urls { url } images { ...I } birth_date death_date ethnicity country
  eye_color hair_color height measurements { band_size cup_size waist hip } breast_type career_start_year career_end_year
  tattoos { location description } piercings { location description } }
query($t: String!) { searchPerformer(term: $t) { ...P } }`, map[string]any{"t": term}, &out); err != nil {
		return nil, err
	}
	res := []Performer{}
	for _, p := range out.Res {
		res = append(res, p.toPerformer(b.Endpoint))
	}
	return res, nil
}

// ---------- ThePornDB ----------

// TPDB talks to api.theporndb.net.
type TPDB struct {
	Key string
	UA  string
}

type tpScene struct {
	ID          json.Number `json:"_id"`
	UUID        string      `json:"id"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Date        string      `json:"date"`
	URL         string      `json:"url"`
	Image       string      `json:"image"`
	Poster      string      `json:"poster"`
	Duration    int         `json:"duration"`
	ExternalID  string      `json:"external_id"`
	Site        *struct {
		Name   string `json:"name"`
		URL    string `json:"url"`
		Logo   string `json:"logo"`
		Parent *struct {
			Name string `json:"name"`
		} `json:"parent"`
	} `json:"site"`
	Performers []struct {
		Name   string `json:"name"`
		Image  string `json:"image"`
		Parent *struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Image  string `json:"image"`
			Extras struct {
				Gender       string `json:"gender"`
				Birthday     string `json:"birthday"`
				Nationality  string `json:"nationality"`
				Ethnicity    string `json:"ethnicity"`
				HairColour   string `json:"hair_colour"`
				EyeColour    string `json:"eye_colour"`
				Height       string `json:"height"`
				Measurements string `json:"measurements"`
				FakeBoobs    *bool  `json:"fake_boobs"`
			} `json:"extras"`
		} `json:"parent"`
	} `json:"performers"`
	Tags []struct {
		Name string `json:"name"`
	} `json:"tags"`
}

func (t *TPDB) get(ctx context.Context, path string, q url.Values, out any) error {
	if t.Key == "" {
		return errors.New("no ThePornDB key — add yours under Settings → Lookups")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.theporndb.net"+path+"?"+q.Encode(), nil)
	req.Header.Set("Authorization", "Bearer "+t.Key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", t.UA)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return errors.New("ThePornDB did not accept the key")
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ThePornDB answered HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(out)
}

func cm(h string) int {
	var n int
	fmt.Sscanf(strings.TrimSpace(h), "%d", &n)
	if n > 0 && n < 100 { // inches
		n = int(float64(n) * 2.54)
	}
	return n
}

func (s tpScene) toScene() Scene {
	out := Scene{Source: "tpdb", Endpoint: "https://theporndb.net", ID: s.UUID, Title: s.Title, Details: s.Description, Date: s.Date,
		Duration: s.Duration, Image: s.Image, Tags: []string{}, Performers: []Performer{}, URLs: []string{}}
	if out.Image == "" {
		out.Image = s.Poster
	}
	if s.URL != "" {
		out.URLs = append(out.URLs, s.URL)
	}
	if s.Site != nil {
		out.Studio = &Studio{Name: s.Site.Name, URL: s.Site.URL, Image: s.Site.Logo}
		if s.Site.Parent != nil && s.Site.Parent.Name != s.Site.Name {
			out.Studio.Parent = s.Site.Parent.Name
		}
	}
	for _, t := range s.Tags {
		out.Tags = append(out.Tags, t.Name)
	}
	for _, p := range s.Performers {
		pp := Performer{Source: "tpdb", Name: p.Name, Image: p.Image}
		if p.Parent != nil {
			pp.ID, pp.Name = p.Parent.ID, firstNonEmpty(p.Parent.Name, p.Name)
			if p.Parent.Name != "" && p.Parent.Name != p.Name {
				pp.As = p.Name
			}
			e := p.Parent.Extras
			pp.Gender, pp.Birthdate, pp.Country, pp.Ethnicity = strings.ToLower(e.Gender), e.Birthday, e.Nationality, strings.ToLower(e.Ethnicity)
			pp.HairColor, pp.EyeColor, pp.HeightCM, pp.Measurements = strings.ToLower(e.HairColour), strings.ToLower(e.EyeColour), cm(e.Height), e.Measurements
			if e.FakeBoobs != nil {
				pp.FakeTits = map[bool]string{true: "Augmented", false: "Natural"}[*e.FakeBoobs]
			}
			if p.Parent.Image != "" {
				pp.Image = p.Parent.Image
			}
		}
		out.Performers = append(out.Performers, pp)
	}
	return out
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// Search finds scenes by the file's hash and/or a name to parse.
func (t *TPDB) Search(ctx context.Context, parse, hash string) ([]Scene, error) {
	res := []Scene{}
	seen := map[string]bool{}
	add := func(list []tpScene, matched string) {
		for _, s := range list {
			if seen[s.UUID] {
				continue
			}
			seen[s.UUID] = true
			sc := s.toScene()
			sc.Matched = matched
			res = append(res, sc)
		}
	}
	if hash != "" {
		var out struct {
			Data json.RawMessage `json:"data"`
		}
		if err := t.get(ctx, "/scenes/hash/"+url.PathEscape(hash), url.Values{}, &out); err != nil {
			return nil, err
		}
		var one tpScene
		var many []tpScene
		if json.Unmarshal(out.Data, &many) == nil {
			add(many, "OSHASH")
		} else if json.Unmarshal(out.Data, &one) == nil && one.UUID != "" {
			add([]tpScene{one}, "OSHASH")
		}
	}
	if parse != "" {
		var out struct {
			Data []tpScene `json:"data"`
		}
		if err := t.get(ctx, "/scenes", url.Values{"parse": {parse}, "per_page": {"12"}}, &out); err != nil {
			return res, err
		}
		add(out.Data, "")
	}
	return res, nil
}

// SearchPerformer finds performers by name.
func (t *TPDB) SearchPerformer(ctx context.Context, term string) ([]Performer, error) {
	var out struct {
		Data []struct {
			ID      string   `json:"id"`
			Name    string   `json:"name"`
			Bio     string   `json:"bio"`
			Image   string   `json:"image"`
			Aliases []string `json:"aliases"`
			Extras  struct {
				Gender       string `json:"gender"`
				Birthday     string `json:"birthday"`
				Nationality  string `json:"nationality"`
				Ethnicity    string `json:"ethnicity"`
				HairColour   string `json:"hair_colour"`
				EyeColour    string `json:"eye_colour"`
				Height       string `json:"height"`
				Measurements string `json:"measurements"`
				Tattoos      string `json:"tattoos"`
				Piercings    string `json:"piercings"`
				FakeBoobs    *bool  `json:"fake_boobs"`
			} `json:"extras"`
		} `json:"data"`
	}
	if err := t.get(ctx, "/performers", url.Values{"q": {term}, "per_page": {"10"}}, &out); err != nil {
		return nil, err
	}
	res := []Performer{}
	for _, p := range out.Data {
		e := p.Extras
		pp := Performer{Source: "tpdb", ID: p.ID, Name: p.Name, Aliases: p.Aliases, Image: p.Image, Gender: strings.ToLower(e.Gender),
			Birthdate: e.Birthday, Country: e.Nationality, Ethnicity: strings.ToLower(e.Ethnicity), HairColor: strings.ToLower(e.HairColour),
			EyeColor: strings.ToLower(e.EyeColour), HeightCM: cm(e.Height), Measurements: e.Measurements, Tattoos: e.Tattoos, Piercings: e.Piercings}
		if e.FakeBoobs != nil {
			pp.FakeTits = map[bool]string{true: "Augmented", false: "Natural"}[*e.FakeBoobs]
		}
		res = append(res, pp)
	}
	return res, nil
}

// Fetch downloads a picture (covers, performer photos) up to 20 MB.
func Fetch(ctx context.Context, u, ua string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ua)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 20<<20))
}
