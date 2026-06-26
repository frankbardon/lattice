package service

import (
	stderrors "errors"
	"io"
	"io/fs"
	"time"
)

// OverlaySchemas returns a read-only fs.FS that serves files from extra when the
// path is present there and falls through to base otherwise. Directory listings
// are MERGED, so the catalog walk (over the afero.FromIOFS adapter the resolver
// wraps the FS in) and Service.ListSchemas both see the union of the two trees. A
// path present in both resolves to extra — the consumer's schema OVERRIDES the
// core schema of the same path.
//
// It is the supported way to inherit lattice's embedded core catalog
// (CoreSchemas) while adding or overriding item/connection types without copying
// the core .schema.json files:
//
//	svc, err := service.Open(service.Options{
//	    Root:    dir,
//	    Schemas: service.OverlaySchemas(service.CoreSchemas(), os.DirFS("myschemas")),
//	})
//
// base and extra must be rooted the same way (dashboard.schema.json at ".",
// item types at items/<name>.schema.json). extra need only carry the files it
// adds or overrides.
func OverlaySchemas(base, extra fs.FS) fs.FS {
	return overlayFS{base: base, extra: extra}
}

// overlayFS composes extra over base. Everything flows through Open because the
// afero.FromIOFS adapter implements ReadFile/ReadDir/Stat on top of Open: an
// overlaid file path opens the extra file, a directory present in either tree
// opens a mergedDir whose ReadDir unions the entries, and any other path falls
// through to base verbatim.
type overlayFS struct {
	base  fs.FS
	extra fs.FS
}

func (o overlayFS) Open(name string) (fs.File, error) {
	if ef, err := o.extra.Open(name); err == nil {
		info, serr := ef.Stat()
		if serr != nil {
			ef.Close()
			return nil, serr
		}
		if !info.IsDir() {
			return ef, nil // extra file overrides the base file of the same path
		}
		ef.Close() // a directory in extra: merge its listing with base's
		return o.openMergedDir(name)
	}

	bf, err := o.base.Open(name)
	if err != nil {
		return nil, err
	}
	info, serr := bf.Stat()
	if serr != nil {
		bf.Close()
		return nil, serr
	}
	if info.IsDir() {
		bf.Close() // base directory: extra may carry children to merge in
		return o.openMergedDir(name)
	}
	return bf, nil
}

// openMergedDir returns a directory whose entries are the union of base's and
// extra's listings of name, with extra entries overriding base entries of the
// same name. Either tree may lack the directory (the missing side contributes no
// entries); at least one exists because the caller only reaches here for a path
// that opened as a directory in base or extra.
func (o overlayFS) openMergedDir(name string) (fs.File, error) {
	baseEntries, _ := fs.ReadDir(o.base, name)
	extraEntries, _ := fs.ReadDir(o.extra, name)

	byName := make(map[string]fs.DirEntry, len(baseEntries)+len(extraEntries))
	order := make([]string, 0, len(baseEntries)+len(extraEntries))
	add := func(e fs.DirEntry) {
		if _, seen := byName[e.Name()]; !seen {
			order = append(order, e.Name())
		}
		byName[e.Name()] = e
	}
	for _, e := range baseEntries {
		add(e)
	}
	for _, e := range extraEntries {
		add(e) // extra wins for a duplicate name
	}

	merged := make([]fs.DirEntry, 0, len(order))
	for _, n := range order {
		merged = append(merged, byName[n])
	}

	var info fs.FileInfo
	if fi, err := fs.Stat(o.base, name); err == nil {
		info = fi
	} else if fi, err := fs.Stat(o.extra, name); err == nil {
		info = fi
	}
	return &mergedDir{name: name, info: info, entries: merged}, nil
}

// mergedDir is an in-memory directory fs.File implementing fs.ReadDirFile so the
// afero adapter's Readdir (and thus afero.Walk + Service.ListSchemas) see the
// unioned entries.
type mergedDir struct {
	name    string
	info    fs.FileInfo
	entries []fs.DirEntry
	pos     int
}

func (d *mergedDir) Stat() (fs.FileInfo, error) {
	if d.info != nil {
		return d.info, nil
	}
	return mergedDirInfo{d.name}, nil
}

func (d *mergedDir) Read([]byte) (int, error) {
	return 0, stderrors.New("is a directory")
}

func (d *mergedDir) Close() error { return nil }

func (d *mergedDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if n <= 0 {
		rest := d.entries[d.pos:]
		d.pos = len(d.entries)
		return rest, nil
	}
	if d.pos >= len(d.entries) {
		return nil, io.EOF
	}
	end := min(d.pos+n, len(d.entries))
	out := d.entries[d.pos:end]
	d.pos = end
	return out, nil
}

// mergedDirInfo is the minimal fs.FileInfo for a synthesized merged directory,
// used only as a fallback when neither tree could stat the directory.
type mergedDirInfo struct{ name string }

func (i mergedDirInfo) Name() string       { return i.name }
func (i mergedDirInfo) Size() int64        { return 0 }
func (i mergedDirInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o555 }
func (i mergedDirInfo) ModTime() time.Time { return time.Time{} }
func (i mergedDirInfo) IsDir() bool        { return true }
func (i mergedDirInfo) Sys() any           { return nil }
