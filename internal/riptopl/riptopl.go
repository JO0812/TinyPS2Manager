package riptopl

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jo/TinyPS2Manager/internal/transfer"
)

// Source of truth for releases.
const (
	GitHubAPI       = "https://api.github.com"
	Repo            = "NathanNeurotic/Open-PS2-Loader"
	DefaultTag      = "current-fan-favorite"
	RollingTag      = "rolling"
	ELFName         = "RIPTOPL.ELF"
	downloadTimeout = 5 * time.Minute // 14MB package on slow links
)

// FlavourPreference follows the release-notes toolchain table.
var FlavourPreference = []string{
	"APP_RIPTOPL-PS2DEVPINNED",
	"APP_RIPTOPL-OFFICIALPINNED",
	"APP_RIPTOPL-PS2DEVROLLING",
	"APP_RIPTOPL-OFFICIALROLLING",
}

// packageAsset matches the installable package, excluding debug/variant/
// language/RA/source zips.
var packageAsset = regexp.MustCompile(`^RIPTOPL-v.*\.zip$`)

func isPackageAsset(name string) bool {
	if !packageAsset.MatchString(name) {
		return false
	}
	upper := strings.ToUpper(name)
	for _, exclude := range []string{"DEBUG", "VARIANTS", "LANGS", "-RA.", "-SRC"} {
		if strings.Contains(upper, exclude) {
			return false
		}
	}
	return true
}

// Release is a resolved downloadable package. AssetURL is surfaced to the
// user BEFORE any fetch (§9.7: explicit per-action network with the URL
// shown); Digest pins what was fetched afterwards.
type Release struct {
	Tag         string
	Commit      string
	PublishedAt string
	AssetName   string
	AssetURL    string
	AssetSize   int64
	Digest      string // lowercase hex sha256 (no prefix)
}

// Client talks to the GitHub releases API. APIBase is overridable for
// tests; HTTP defaults to a download-sized timeout.
type Client struct {
	APIBase string
	Repo    string
	HTTP    *http.Client
}

func (c *Client) base() string {
	if c.APIBase != "" {
		return c.APIBase
	}
	return GitHubAPI
}

func (c *Client) repo() string {
	if c.Repo != "" {
		return c.Repo
	}
	return Repo
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: downloadTimeout}
}

type apiAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	Digest             string `json:"digest"`
}

type apiRelease struct {
	TagName      string     `json:"tag_name"`
	TargetCommit string     `json:"target_commitish"`
	PublishedAt  string     `json:"published_at"`
	Assets       []apiAsset `json:"assets"`
}

// Resolve fetches the release metadata for tag and selects the installable
// package. No bytes download here: the caller shows AssetURL first.
func (c *Client) Resolve(ctx context.Context, tag string) (*Release, error) {
	if tag == "" {
		tag = DefaultTag
	}
	url := fmt.Sprintf("%s/repos/%s/releases/tags/%s", c.base(), c.repo(), tag)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("list RiptOPL releases: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list RiptOPL releases: HTTP %d (tag %q?)", resp.StatusCode, tag)
	}
	var rel apiRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("parse release metadata: %w", err)
	}
	for _, a := range rel.Assets {
		if !isPackageAsset(a.Name) || a.BrowserDownloadURL == "" {
			continue
		}
		digest, err := parseDigest(a.Digest)
		if err != nil {
			return nil, fmt.Errorf("asset %s: %w", a.Name, err)
		}
		return &Release{
			Tag: rel.TagName, Commit: rel.TargetCommit, PublishedAt: rel.PublishedAt,
			AssetName: a.Name, AssetURL: a.BrowserDownloadURL,
			AssetSize: a.Size, Digest: digest,
		}, nil
	}
	return nil, fmt.Errorf("tag %q has no installable RIPTOPL-*.zip package", tag)
}

func parseDigest(d string) (string, error) {
	digest := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(d)), "sha256:")
	if len(digest) != 64 {
		return "", fmt.Errorf("bad asset digest %q", d)
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return "", fmt.Errorf("bad asset digest %q", d)
	}
	return digest, nil
}

// Download streams the package to a temp file, verifying size and the
// API-published digest. Progress reports cumulative bytes (nil-safe); the
// caller deletes the file.
func (c *Client) Download(ctx context.Context, rel *Release, onProgress func(done, total int64)) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", rel.AssetURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := c.http().Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", rel.AssetName, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: HTTP %d", rel.AssetName, resp.StatusCode)
	}
	tmp, err := os.CreateTemp("", "riptopl-*.zip")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(name)
		}
	}()
	h := sha256.New()
	var done int64
	buf := make([]byte, 1<<20)
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := tmp.Write(buf[:n]); werr != nil {
				return "", fmt.Errorf("stage download: %w", werr)
			}
			h.Write(buf[:n])
			done += int64(n)
			if onProgress != nil {
				onProgress(done, rel.AssetSize)
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				break
			}
			return "", fmt.Errorf("download %s: %w", rel.AssetName, rerr)
		}
	}
	if rel.AssetSize > 0 && done != rel.AssetSize {
		return "", fmt.Errorf("download %s: got %d of %d bytes", rel.AssetName, done, rel.AssetSize)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != rel.Digest {
		return "", fmt.Errorf("download %s: digest mismatch (want %s)", rel.AssetName, rel.Digest)
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	ok = true
	return name, nil
}

// Staged describes a placed loader.
type Staged struct {
	Release
	Flavour string // e.g. APP_RIPTOPL-PS2DEVPINNED
	ELFPath string // destination-absolute staged path
	ELFSize int64
}

// Stage extracts the preferred flavour's ELF from the package zip and
// places it at <root>/<prefix>/APPS/<flavour>/RIPTOPL.ELF via disk
// (same-volume temp + rename, like every other destination write).
func Stage(ctx context.Context, disk transfer.Disk, root, prefix, zipPath string, flavours []string) (*Staged, error) {
	if flavours == nil {
		flavours = FlavourPreference
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("open package: %w", err)
	}
	defer zr.Close()
	for _, flavour := range flavours {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		entry := findFlavourELF(zr.File, flavour)
		if entry == nil {
			continue
		}
		return stageEntry(ctx, disk, root, prefix, flavour, entry)
	}
	return nil, fmt.Errorf("package has none of the known flavours %v", flavours)
}

// findFlavourELF locates APPS/<flavour>/RIPTOPL.ELF, rejecting zip-slip
// entries and case variants (OPL paths are case-sensitive).
func findFlavourELF(files []*zip.File, flavour string) *zip.File {
	want := "APPS/" + flavour + "/" + ELFName
	for _, f := range files {
		if f.Name != want || f.FileInfo().IsDir() {
			continue
		}
		return f
	}
	return nil
}

func stageEntry(ctx context.Context, disk transfer.Disk, root, prefix, flavour string, entry *zip.File) (*Staged, error) {
	if entry.UncompressedSize64 > 64<<20 {
		return nil, fmt.Errorf("ELF entry suspiciously large (%d bytes)", entry.UncompressedSize64)
	}
	rc, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	dir := filepath.Join("APPS", flavour)
	if prefix != "" {
		dir = filepath.Join(prefix, dir)
	}
	absDir := filepath.Join(root, dir)
	if err := disk.MkdirAll(absDir); err != nil {
		return nil, err
	}
	// Zip-slip guard on the constructed path (defense in depth: the name
	// is fully constructed here, but never trust path joins blindly).
	final := filepath.Join(root, dir, ELFName)
	if !pathIsInside(root, final) {
		return nil, fmt.Errorf("refusing to stage outside %s", root)
	}
	if err := disk.CopyToDest(ctx, final, rc, int64(entry.UncompressedSize64), nil); err != nil {
		return nil, fmt.Errorf("stage %s: %w", flavour, err)
	}
	fi, err := disk.Stat(final)
	if err != nil {
		return nil, err
	}
	return &Staged{Flavour: flavour, ELFPath: final, ELFSize: fi.Size()}, nil
}

// pathIsInside reports whether target resolves inside root.
func pathIsInside(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Checklist returns the ordered first-boot steps (spec §2.7): the drive is
// never assumed ready — the UI renders these as a per-destination checklist.
func Checklist() []string {
	return []string{
		"On the PS2, launch RIPTOPL.ELF from your homebrew launcher (FMCB, FHDB, or equivalent).",
		"Open Settings → Game Sources and enable only the devices you use (USB, MX4SIO, iLink, SMB, HDD).",
		"Pick a start mode (Manual or Auto), then Save Changes — this writes settings_riptopl.cfg and creates DVD/, CD/, ART/, CFG/, VMC/ (never POPS/).",
		"Press L3 to reach PS1 titles: the game list defaults to separate PS2/PS1 views.",
	}
}
