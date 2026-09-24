package art

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/jo/TinyPS2Manager/internal/library"
	"github.com/jo/TinyPS2Manager/internal/transfer"
)

// Target dimensions (spec §2.8: 512×730 PS2 portrait, ~512×512 PS1 square).
const (
	PS2Width  = 512
	PS2Height = 730
	PS1Width  = 512
	PS1Height = 512
)

// Source pattern constants (spec §2.8, documented). No HTML scraping;
// these are the exact per-system raw URLs.
const (
	// PS1 libretro thumbnails per-system repo (parent is only .gitmodules).
	PS1LibretroBase = "https://raw.githubusercontent.com/libretro-thumbnails/Sony_-_PlayStation/master/Named_Boxarts"
	// PS2 GameTDB cover base; region segment varies.
	PS2GameTDBBase = "https://art.gametdb.com/ps2/cover"
)

var pngMagic = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

// KeyFor returns the ART filename for an item. isEmber distinguishes the
// Ember path (folder-based) from POPSTARTER VCD. For PS2, GameID is required;
// when missing it falls back to a sanitized title. For PS1 POPSTARTER, the
// VCD basename (without .VCD) is used; when that basename itself looks like
// SXXX_NNN.NN.Title, the PS1-ID form is kept verbatim. For Ember, folder is
// the EMBER/games/<Name> folder name.
func KeyFor(item library.LibraryItem, vcdName string, emberFolder string, isEmber bool) string {
	if isEmber {
		folder := emberFolder
		if folder == "" {
			folder = item.Title
		}
		folder = sanitize(folder)
		if folder == "" {
			folder = "Unknown"
		}
		return folder + "_COV.png"
	}
	if item.Platform == library.PlatformPS2 {
		if item.GameID != "" {
			return item.GameID + "_COV.png"
		}
		// Fallback: sanitized title (still usable, just not join-keyed).
		t := sanitize(item.Title)
		if t == "" {
			t = "Unknown"
		}
		return t + "_COV.png"
	}
	// PS1 POPSTARTER
	if vcdName != "" {
		base := strings.TrimSuffix(vcdName, ".VCD")
		base = strings.TrimSuffix(base, ".vcd")
		// Keep PS1-ID form verbatim (SXXX_NNN.NN.Title.VCD); sanitize anyway
		// for filesystem safety but preserve the ID prefix.
		return sanitize(base) + "_COV.png"
	}
	// Fallback to title
	t := sanitize(item.Title)
	if t == "" {
		t = "Unknown"
	}
	return t + "_COV.png"
}

// sanitize replaces forbidden characters (/ \ : * ? " < > |) and trims spaces.
// It keeps the case as-is (ART filenames are case-sensitive per spec) but
// replaces separators with underscore.
func sanitize(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	// Limit length: ART keys target ≤ 64 chars inc _COV.png; keep sane.
	out := b.String()
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

// PS2CoverURLs returns GameTDB cover URLs with region priority. Region is
// derived from title tags (USA→US/EN, Europe→EN, Japan→JA) but all
// variants are returned in priority order so callers can try sequentially.
func PS2CoverURLs(gameID string) []string {
	if gameID == "" {
		return nil
	}
	// GameTDB expects ID without separators: SLUS_213.85 -> SLUS21385
	flat := strings.ReplaceAll(strings.ReplaceAll(gameID, "_", ""), ".", "")
	flat = strings.ToUpper(flat)
	regions := []string{"EN", "US", "JA", "FR", "DE", "ES", "IT", "UK", "PT", "KO", "ZH"}
	var urls []string
	for _, r := range regions {
		u := fmt.Sprintf("%s/%s/%s.png", PS2GameTDBBase, r, flat)
		urls = append(urls, u)
	}
	return urls
}

// PS1CoverURLs returns libretro thumbnail URLs for a PS1 title. The title
// is URL-escaped and tried with .png extension.
func PS1CoverURLs(title string) []string {
	t := strings.TrimSpace(title)
	if t == "" {
		return nil
	}
	esc := url.PathEscape(t)
	return []string{
		fmt.Sprintf("%s/%s.png", PS1LibretroBase, esc),
	}
}

// Client fetches cover art with timeout + retries. APIBase overrides the
// base for tests (empty = real constants).
type Client struct {
	HTTP *http.Client
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// Fetch downloads url with context, validates PNG magic and decodes config
// for dimension checks. It retries up to 3 times on transient errors (network
// or 5xx); 4xx and bad PNG are returned immediately.
func (c *Client) Fetch(ctx context.Context, artURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		data, err := c.fetchOnce(ctx, artURL)
		if err == nil {
			return data, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Don't retry on client errors or bad PNG (not transient).
		msg := err.Error()
		if strings.Contains(msg, "HTTP 4") || strings.Contains(msg, "not a PNG") || strings.Contains(msg, "png decode") {
			return nil, fmt.Errorf("fetch %s: %w", artURL, err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 300 * time.Millisecond):
		}
	}
	return nil, fmt.Errorf("fetch %s: %w", artURL, lastErr)
}

func (c *Client) fetchOnce(ctx context.Context, artURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", artURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "oplbm/ art fetch")
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	// Limit to 5 MiB (covers are tiny; prevents abuse).
	limited := io.LimitReader(resp.Body, 5<<20)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(data) < 8 || !bytes.Equal(data[:8], pngMagic) {
		return nil, fmt.Errorf("not a PNG (bad magic)")
	}
	// Validate dimensions via DecodeConfig (does not hold full image).
	if _, err := png.DecodeConfig(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("png decode config: %w", err)
	}
	return data, nil
}

// ValidateAndNormalize checks PNG magic + decodes, then scales to the target
// size with fit-not-stretch (preserve aspect, centered). isPS2 selects the
// target (512×730 vs 512×512). It returns the normalized PNG bytes.
func ValidateAndNormalize(pngData []byte, isPS2 bool) ([]byte, error) {
	if len(pngData) < 8 || !bytes.Equal(pngData[:8], pngMagic) {
		return nil, fmt.Errorf("not a PNG")
	}
	img, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return nil, fmt.Errorf("png decode: %w", err)
	}
	tw, th := PS1Width, PS1Height
	if isPS2 {
		tw, th = PS2Width, PS2Height
	}
	normalized := fitNotStretch(img, tw, th)
	var buf bytes.Buffer
	if err := png.Encode(&buf, normalized); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func fitNotStretch(src image.Image, tw, th int) image.Image {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	if sw == tw && sh == th {
		return src
	}
	// Scale to fit inside target, preserve aspect.
	scaleW := float64(tw) / float64(sw)
	scaleH := float64(th) / float64(sh)
	scale := scaleW
	if scaleH < scaleW {
		scale = scaleH
	}
	nw := int(float64(sw) * scale)
	nh := int(float64(sh) * scale)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	scaled := nearestNeighborScale(src, nw, nh)
	// Center on target canvas (white background).
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	// Fill with dark gray (theme neutral).
	draw.Draw(dst, dst.Bounds(), &image.Uniform{color.RGBA{32, 32, 32, 255}}, image.Point{}, draw.Src)
	offX := (tw - nw) / 2
	offY := (th - nh) / 2
	draw.Draw(dst, image.Rect(offX, offY, offX+nw, offY+nh), scaled, image.Point{}, draw.Over)
	return dst
}

func nearestNeighborScale(src image.Image, tw, th int) image.Image {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	for y := 0; y < th; y++ {
		sy := y * sh / th
		for x := 0; x < tw; x++ {
			sx := x * sw / tw
			dst.Set(x, y, src.At(sb.Min.X+sx, sb.Min.Y+sy))
		}
	}
	return dst
}

// GenerateCustom creates a local cover (512×730 or 512×512) with a solid
// color derived from title hash and a simple border. It is flagged
// customArt:true so updates never overwrite it with a stock download.
func GenerateCustom(title string, isPS2 bool) []byte {
	tw, th := PS1Width, PS1Height
	if isPS2 {
		tw, th = PS2Width, PS2Height
	}
	// Color from title hash.
	h := sha256.Sum256([]byte(strings.ToLower(title)))
	c := color.RGBA{R: h[0], G: h[1], B: h[2], A: 255}
	// Ensure not too dark.
	if c.R < 40 && c.G < 40 && c.B < 40 {
		c.R += 60
		c.G += 60
		c.B += 60
	}
	img := image.NewRGBA(image.Rect(0, 0, tw, th))
	draw.Draw(img, img.Bounds(), &image.Uniform{c}, image.Point{}, draw.Src)
	// Border
	border := color.RGBA{255, 255, 255, 180}
	for x := 0; x < tw; x++ {
		img.Set(x, 0, border)
		img.Set(x, th-1, border)
	}
	for y := 0; y < th; y++ {
		img.Set(0, y, border)
		img.Set(tw-1, y, border)
	}
	// Simple title hash pattern in center (avoid text rendering without font).
	// Draw a smaller inset rectangle with inverted color to hint "custom".
	inset := 20
	inv := color.RGBA{R: 255 - c.R, G: 255 - c.G, B: 255 - c.B, A: 255}
	draw.Draw(img, image.Rect(inset, inset, tw-inset, th-inset), &image.Uniform{inv}, image.Point{}, draw.Src)
	// Re-fill inner with original to create frame effect
	draw.Draw(img, image.Rect(inset+4, inset+4, tw-inset-4, th-inset-4), &image.Uniform{c}, image.Point{}, draw.Src)

	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// Stage writes pngData to <root>/<prefix>/ART/<key> via disk (same-volume
// temp + rename). If a file already exists at that path, it is not
// overwritten — hand files in the watched folder always win (spec §2.8 #3).
func Stage(ctx context.Context, disk transfer.Disk, root, prefix string, key string, pngData []byte) error {
	dir := filepath.Join("ART")
	if prefix != "" {
		dir = filepath.Join(prefix, dir)
	}
	absDir := filepath.Join(root, dir)
	if err := disk.MkdirAll(absDir); err != nil {
		return err
	}
	final := filepath.Join(root, dir, key)
	if _, err := disk.Stat(final); err == nil {
		// Already exists: hand file wins, never overwrite.
		return nil
	}
	return disk.CopyToDest(ctx, final, bytes.NewReader(pngData), int64(len(pngData)), nil)
}
