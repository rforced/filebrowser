package search

import (
	"mime"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	typeRegexp = regexp.MustCompile(`type:(\w+)`)
)

type condition func(path string) bool

func extensionCondition(extension string) condition {
	return func(path string) bool {
		return filepath.Ext(path) == "."+extension
	}
}

func imageCondition(path string) bool {
	extension := filepath.Ext(path)
	mimetype := mime.TypeByExtension(extension)

	return strings.HasPrefix(mimetype, "image")
}

func audioCondition(path string) bool {
	extension := filepath.Ext(path)
	mimetype := mime.TypeByExtension(extension)

	return strings.HasPrefix(mimetype, "audio")
}

func videoCondition(path string) bool {
	extension := filepath.Ext(path)
	mimetype := mime.TypeByExtension(extension)

	return strings.HasPrefix(mimetype, "video")
}

// fieldCondition matches 3D field output: post*.h5 snapshots and any CGNS,
// which unlike HDF5 is always CFD field data. A bare *.h5 is not enough —
// CONVERGE also uses the container for restarts, mapped initial conditions and
// lookup tables, which are not what someone searching for fields wants.
func fieldCondition(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	if strings.HasSuffix(name, ".cgns") {
		return true
	}
	return strings.HasPrefix(name, "post") && strings.HasSuffix(name, ".h5")
}

func parseSearch(value string) *searchOptions {
	opts := &searchOptions{
		CaseSensitive: strings.Contains(value, "case:sensitive"),
		Conditions:    []condition{},
		Terms:         []string{},
	}

	// removes the options from the value
	value = strings.ReplaceAll(value, "case:insensitive", "")
	value = strings.ReplaceAll(value, "case:sensitive", "")
	value = strings.TrimSpace(value)

	types := typeRegexp.FindAllStringSubmatch(value, -1)
	for _, t := range types {
		if len(t) == 1 {
			continue
		}

		switch t[1] {
		case "image":
			opts.Conditions = append(opts.Conditions, imageCondition)
		case "audio", "music":
			opts.Conditions = append(opts.Conditions, audioCondition)
		case "video":
			opts.Conditions = append(opts.Conditions, videoCondition)
		case "input", "inputs":
			opts.Conditions = append(opts.Conditions, extensionCondition("in"))
		case "output", "outputs":
			opts.Conditions = append(opts.Conditions, extensionCondition("out"))
		case "log", "logs":
			opts.Conditions = append(opts.Conditions, extensionCondition("log"))
		case "restart", "restarts":
			opts.Conditions = append(opts.Conditions, extensionCondition("rst"))
		case "field", "fields":
			opts.Conditions = append(opts.Conditions, fieldCondition)
		default:
			opts.Conditions = append(opts.Conditions, extensionCondition(t[1]))
		}
	}

	if len(types) > 0 {
		// Remove the fields from the search value.
		value = typeRegexp.ReplaceAllString(value, "")
	}

	// If it's case insensitive, put everything in lowercase.
	if !opts.CaseSensitive {
		value = strings.ToLower(value)
	}

	// Remove the spaces from the search value.
	value = strings.TrimSpace(value)

	if value == "" {
		return opts
	}

	// if the value starts with " and finishes what that character, we will
	// only search for that term
	if value[0] == '"' && value[len(value)-1] == '"' {
		unique := strings.TrimPrefix(value, "\"")
		unique = strings.TrimSuffix(unique, "\"")

		opts.Terms = []string{unique}
		return opts
	}

	opts.Terms = strings.Split(value, " ")
	return opts
}
