package fonts

import (
	"fmt"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Discover collects the monospace families on this machine, so the
// settings UI has something to offer. The first one is the default.
//
// Every face in every candidate file is opened and filed under the family name
// it reports. That is the only thing a four-file family and a four-face
// collection have in common, and it costs one open per file at startup.
func Discover() []*Family {
	byName := map[string]*Family{}
	var order []string

	add := func(path string) {
		sources, err := openCollection(path)
		if err != nil {
			return
		}
		for _, src := range sources {
			meta := src.Metadata()
			name := displayName(meta.Family)
			if name == "" {
				continue
			}
			family, ok := byName[name]
			if !ok {
				family = &Family{Name: name}
				byName[name] = family
				order = append(order, name)
			}
			i := 0
			// Semibold and up counts as the bold face. Below that a family
			// with several light weights would fill the bold slot with one.
			if meta.Weight >= text.WeightSemibold {
				i |= faceBold
			}
			if meta.Style == text.StyleItalic {
				i |= faceItalic
			}
			// First one wins, so a family with several weights keeps the one
			// nearest regular rather than the last file read.
			if family.sources[i] == nil {
				family.sources[i] = src
			}
		}
	}

	for _, path := range fontFiles() {
		add(path)
	}

	families := make([]*Family, 0, len(order))
	for _, name := range order {
		// A family with no regular face is one this cannot draw with.
		if byName[name].sources[0] != nil {
			families = append(families, byName[name])
		}
	}
	// The candidates are in preference order and the override comes first, so
	// only what the directory scan found is sorted, which is everything after
	// the fixed list.
	fixed := len(monoCandidates())
	if os.Getenv("GOSTTY_FONT") != "" {
		fixed++
	}
	if fixed < len(families) {
		rest := families[min(fixed, len(families)):]
		sort.Slice(rest, func(i, j int) bool { return rest[i].Name < rest[j].Name })
	}
	return families
}

// The families to start with, in the order they are worth having, if any of
// them is on the machine.
//
// JetBrains Mono first because it is a terminal font: it has all four faces, a
// tall x-height and letterforms that stay apart at small sizes, which is more
// than can be said for what most systems ship. The Nerd Font builds come in
// three widths and only the "Mono" one keeps its icons inside a single cell,
// which is the one a grid can use.
var preferredFamilies = []string{
	"JetBrains Mono",
	"JetBrainsMono NFM",
	"JetBrainsMono NF",
	"JetBrainsMonoNL NFM",
	"JetBrainsMonoNL NF",
}

// DefaultFamily picks what the window opens with, and where it sits in the list
// the settings panel offers.
//
// GOSTTY_FONT wins outright: someone who named a font meant it. Otherwise the
// preferred families are tried in order, then anything else by the same name,
// and failing all of that the first font found on the system.
func DefaultFamily(families []*Family) (*Family, int) {
	if len(families) == 0 {
		return nil, 0
	}
	if os.Getenv("GOSTTY_FONT") != "" {
		return families[0], 0
	}
	for _, want := range preferredFamilies {
		for i, family := range families {
			if sameFamily(family.Name, want) {
				return family, i
			}
		}
	}
	// A build of the same family under a name not listed above.
	for i, family := range families {
		if strings.HasPrefix(normalizeFamily(family.Name), normalizeFamily(preferredFamilies[0])) {
			return family, i
		}
	}
	return families[0], 0
}

func sameFamily(a, b string) bool {
	return normalizeFamily(a) == normalizeFamily(b)
}

// normalizeFamily takes the case and the spaces out, since the same family is
// written several ways: "JetBrains Mono", "JetBrainsMono NFM".
func normalizeFamily(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, " ", ""))
}

// displayName tidies the name a font reports. macOS hides its system fonts
// behind a leading dot (".SF NS Mono"), which is a packaging detail rather than
// something to show a user.
func displayName(family string) string {
	return strings.TrimPrefix(strings.TrimSpace(family), ".")
}

// fontFiles is where to look for monospace fonts: the known system paths first,
// then whatever the user installed themselves, filtered by name because opening
// every font on the machine to find out is not worth the startup.
func fontFiles() []string {
	var paths []string
	if override := os.Getenv("GOSTTY_FONT"); override != "" {
		paths = append(paths, override)
	}
	paths = append(paths, monoCandidates()...)
	for _, dir := range userFontDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			if !fontExtension(name) || !strings.Contains(strings.ToLower(name), "mono") {
				continue
			}
			paths = append(paths, filepath.Join(dir, name))
		}
	}
	return paths
}

func fontExtension(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".ttf", ".otf", ".ttc", ".otc":
		return true
	}
	return false
}

func userFontDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	switch runtime.GOOS {
	case "darwin":
		return []string{filepath.Join(home, "Library/Fonts"), "/Library/Fonts"}
	case "windows":
		return []string{filepath.Join(home, `AppData\Local\Microsoft\Windows\Fonts`)}
	default:
		return []string{
			filepath.Join(home, ".local/share/fonts"),
			filepath.Join(home, ".fonts"),
		}
	}
}

func openFirst(override string, candidates []string) *text.GoTextFaceSource {
	if override != "" {
		src, err := openFont(override)
		if err == nil {
			return src
		}
		fmt.Fprintf(os.Stderr, "gostty: %s: %v\n", override, err)
	}
	for _, path := range candidates {
		if src, err := openFont(path); err == nil {
			return src
		}
	}
	return nil
}

func openFont(path string) (*text.GoTextFaceSource, error) {
	sources, err := openCollection(path)
	if err != nil {
		return nil, err
	}
	// A .ttc holds several faces; the first is the regular one in every
	// collection this looks at.
	return sources[0], nil
}

func openCollection(path string) ([]*text.GoTextFaceSource, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sources, err := text.NewGoTextFaceSourcesFromCollection(f)
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("%s: no faces", path)
	}
	return sources, nil
}

func monoCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/System/Library/Fonts/Menlo.ttc",
			"/System/Library/Fonts/SFNSMono.ttf",
			"/System/Library/Fonts/SFNSMonoItalic.ttf",
			"/System/Library/Fonts/Monaco.ttf",
			"/System/Library/Fonts/Supplemental/Andale Mono.ttf",
			"/System/Library/Fonts/Supplemental/PTMono.ttc",
			"/System/Library/Fonts/Supplemental/Courier New.ttf",
			"/System/Library/Fonts/Supplemental/Courier New Bold.ttf",
			"/System/Library/Fonts/Supplemental/Courier New Italic.ttf",
			"/System/Library/Fonts/Supplemental/Courier New Bold Italic.ttf",
		}
	case "windows":
		return []string{
			`C:\Windows\Fonts\consola.ttf`,
			`C:\Windows\Fonts\consolab.ttf`,
			`C:\Windows\Fonts\consolai.ttf`,
			`C:\Windows\Fonts\consolaz.ttf`,
			`C:\Windows\Fonts\cour.ttf`,
			`C:\Windows\Fonts\courbd.ttf`,
			`C:\Windows\Fonts\couri.ttf`,
			`C:\Windows\Fonts\courbi.ttf`,
		}
	default:
		return []string{
			"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
			"/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf",
			"/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Oblique.ttf",
			"/usr/share/fonts/truetype/dejavu/DejaVuSansMono-BoldOblique.ttf",
			"/usr/share/fonts/TTF/DejaVuSansMono.ttf",
			"/usr/share/fonts/truetype/liberation/LiberationMono-Regular.ttf",
			"/usr/share/fonts/truetype/liberation/LiberationMono-Bold.ttf",
			"/usr/share/fonts/truetype/liberation/LiberationMono-Italic.ttf",
			"/usr/share/fonts/truetype/liberation/LiberationMono-BoldItalic.ttf",
			"/usr/share/fonts/google-noto/NotoSansMono-Regular.ttf",
			"/usr/share/fonts/truetype/noto/NotoSansMono-Regular.ttf",
		}
	}
}

func wideCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/System/Library/Fonts/AppleSDGothicNeo.ttc",
			"/System/Library/Fonts/Supplemental/AppleGothic.ttf",
			"/System/Library/Fonts/Hiragino Sans GB.ttc",
		}
	case "windows":
		return []string{
			`C:\Windows\Fonts\malgun.ttf`,
			`C:\Windows\Fonts\msgothic.ttc`,
		}
	default:
		return []string{
			"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/noto-cjk/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/truetype/noto/NotoSansKR-Regular.otf",
		}
	}
}
