package mcp_test

// End-to-end proof of the lattice-skill:// resources (E1-S3). It drives the MCP
// server through the SDK's in-memory transport pair and asserts the host can
// enumerate one resource per embedded skill (URI lattice-skill://<name>, MIME
// text/markdown, description = the skill's frontmatter description) and read a
// resource back as the skill's verbatim markdown body — the same source get_skill
// serves. Registration is driven by skills.List, so the test derives its
// expectations from the corpus rather than hardcoding skill names.

import (
	"context"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/frankbardon/lattice/mcp/skills"
)

// skillResourceURIScheme mirrors the unexported scheme the resource registrar
// uses; the test reconstructs expected URIs from skills.List the same way.
const skillResourceURIScheme = "lattice-skill://"

// TestSkillResourcesListed asserts every embedded skill is enumerated as a
// resource with the expected URI, MIME type, and frontmatter description.
func TestSkillResourcesListed(t *testing.T) {
	cs := newTestSession(t)

	res, err := cs.ListResources(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}

	got := make(map[string]*sdkmcp.Resource, len(res.Resources))
	for _, r := range res.Resources {
		got[r.URI] = r
	}

	corpus := skills.List()
	if len(corpus) == 0 {
		t.Fatal("skills.List() returned no skills; cannot exercise resource enumeration")
	}
	for _, meta := range corpus {
		uri := skillResourceURIScheme + meta.Name
		r, ok := got[uri]
		if !ok {
			t.Errorf("skill %q not listed as resource %q", meta.Name, uri)
			continue
		}
		if r.MIMEType != "text/markdown" {
			t.Errorf("resource %q MIMEType = %q, want text/markdown", uri, r.MIMEType)
		}
		if r.Description != meta.Description {
			t.Errorf("resource %q Description = %q, want %q", uri, r.Description, meta.Description)
		}
	}
}

// TestReadSkillResource asserts reading a skill resource returns the skill's
// verbatim markdown body — the same source get_skill / skills.Get returns.
func TestReadSkillResource(t *testing.T) {
	cs := newTestSession(t)

	corpus := skills.List()
	if len(corpus) == 0 {
		t.Fatal("skills.List() returned no skills; cannot exercise a resource read")
	}
	name := corpus[0].Name
	uri := skillResourceURIScheme + name

	want, ok := skills.Get(name)
	if !ok {
		t.Fatalf("skills.Get(%q) miss; corpus and resource source disagree", name)
	}

	res, err := cs.ReadResource(context.Background(), &sdkmcp.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("ReadResource %q: %v", uri, err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("contents = %d, want 1", len(res.Contents))
	}
	c := res.Contents[0]
	if c.URI != uri {
		t.Errorf("content URI = %q, want %q", c.URI, uri)
	}
	if c.MIMEType != "text/markdown" {
		t.Errorf("content MIMEType = %q, want text/markdown", c.MIMEType)
	}
	if c.Text != want {
		t.Errorf("content body does not match skills.Get(%q)", name)
	}
}
