---
name: TinyPS2Manager
description: OPL Backup Manager — the safe, correct way to prepare a PS2/PS1 library for OPL / RiptOPL.
colors:
  abyss: "#070b14"
  bench: "#0a101c"
  tray: "#0e1626"
  tray-lift: "#152238"
  seam: "#1e2f4d"
  console-white: "#e8eefc"
  dial-gray: "#8b98b0"
  faint-trace: "#55627a"
  cathode-glow: "#8fbfff"
  cathode-bright: "#a9d0ff"
  cathode-wash: "rgba(143, 191, 255, 0.12)"
  signal-edge: "#4a86c8"
  confirm-mint: "#34d399"
  confirm-wash: "rgba(52, 211, 153, 0.14)"
  fault-coral: "#f87171"
  fault-wash: "rgba(248, 113, 113, 0.14)"
  caution-amber: "#fbbf24"
typography:
  display:
    fontFamily: "system-ui, -apple-system, Segoe UI, Roboto, sans-serif"
    fontSize: "44px"
    fontWeight: 800
    lineHeight: 1
  headline:
    fontFamily: "system-ui, -apple-system, Segoe UI, Roboto, sans-serif"
    fontSize: "28px"
    fontWeight: 700
    lineHeight: 1.45
    letterSpacing: "-0.01em"
  title:
    fontFamily: "system-ui, -apple-system, Segoe UI, Roboto, sans-serif"
    fontSize: "15px"
    fontWeight: 700
    lineHeight: 1.45
  body:
    fontFamily: "system-ui, -apple-system, Segoe UI, Roboto, sans-serif"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: 1.45
  label:
    fontFamily: "system-ui, -apple-system, Segoe UI, Roboto, sans-serif"
    fontSize: "12px"
    fontWeight: 600
    lineHeight: 1.45
rounded:
  sm: "8px"
  md: "10px"
  lg: "12px"
  pill: "999px"
spacing:
  xs: "4px"
  sm: "8px"
  md: "16px"
  lg: "20px"
  xl: "36px"
components:
  button-primary:
    backgroundColor: "{colors.cathode-glow}"
    textColor: "{colors.bench}"
    rounded: "{rounded.md}"
    padding: "10px 18px"
  button-primary-hover:
    backgroundColor: "{colors.cathode-bright}"
  button-ghost:
    backgroundColor: "{colors.tray}"
    textColor: "{colors.dial-gray}"
    rounded: "{rounded.md}"
    padding: "8px 14px"
  button-ghost-hover:
    backgroundColor: "{colors.tray-lift}"
    textColor: "{colors.console-white}"
  button-danger-ghost:
    textColor: "{colors.fault-coral}"
    rounded: "{rounded.sm}"
    padding: "6px 12px"
  card:
    backgroundColor: "{colors.tray}"
    rounded: "{rounded.lg}"
  input-field:
    backgroundColor: "{colors.tray}"
    textColor: "{colors.console-white}"
    rounded: "{rounded.md}"
    padding: "10px 14px"
  pill:
    rounded: "{rounded.pill}"
    padding: "4px 10px"
  progress-fill:
    backgroundColor: "{colors.cathode-glow}"
---

# Design System: TinyPS2Manager

## Overview

**Creative North Star: "The Night Workbench"**

A phosphor-lit disc lab from the PS2 era, kept honest: near-black navy benches, one cathode glow for everything that acts or reports, and status tints that read like lamps on a test rig. Density is operational — a fixed icon rail, a drive-status strip, and trays of game cards — with hierarchy carried by faint-to-white text steps rather than decoration. Nothing here entertains; every pixel either identifies a disc, reports a state, or offers the next safe action.

The system is dark-only and unapologetic about it: the bench is where the user already is, and the UI behaves like the equipment sitting on it — flat tonal panels, tactile chunky controls, progress bars that crawl at transfer speed like a write LED. Staged covers face out like discs on a shelf; titles without staged art show monogram blanks in the blue family.

**Key Characteristics:**
- Dark-only instrument panel on a four-step navy tonal scale.
- One accent hue for action and signal; status shown by soft-tint pills, never by new hues.
- Tactile, confident controls: chunky radii, bold pills, hover states that lift one tonal step.
- Flat by default; a single deep shadow exists solely for floating menus.

## Colors

A cold, sparse lab palette: near-black navy benches, one cathode blue for action, and three lamp colors for status.

### Primary
- **Cathode Glow Blue** (#8fbfff): the only hue that means action or signal — primary buttons, links, progress fills, active drive icon, logo tile. Brightens to #a9d0ff on hover; washes to a 12% tint for active-rail backgrounds.

### Neutral
- **Abyss Navy** (#070b14): app ground.
- **Bench Navy** (#0a101c): raised chrome — sidebar rail, menus, select boxes, mini progress beds.
- **Work Tray** (#0e1626): cards, fields, drivebar, toolbar wells.
- **Tray Lift** (#152238): the single hover step — every ghost button, nav item, and menu row lands here.
- **Seam Blue** (#1e2f4d): the one border color for all hairlines, card edges, and field strokes.
- **Console White** (#e8eefc): primary text.
- **Dial Gray** (#8b98b0): secondary text, ghost-button labels, unselected icons.
- **Faint Trace** (#55627a): tertiary text — placeholders, counts, keyboard hints, disconnected dots.

### Tertiary
- **Confirm Mint** (#34d399, wash 14%): done, found, connected, pass.
- **Fault Coral** (#f87171, wash 14%): error, destructive text, unreachable, fail.
- **Caution Amber** (#fbbf24, wash ~10–16%): uncertain GameID, region doubt, unplugged drive, warn.

### Named Rules
**The Dark-Only Rule.** There is no light mode; `theme.ts` pins dark unconditionally and `color-scheme` is dark. Never design a light variant.
**The One Glow Rule.** Cathode Glow Blue is the only blue that acts. Tile gradients and cover placeholders may live in the blue family, but no second UI accent may be introduced.

## Typography

**Display Font:** System stack (system-ui first, Segoe UI / Roboto fallbacks)
**Body Font:** System stack (same)
**Label/Mono Font:** No distinct mono face; `kbd` hints reuse the UI stack at 11px.

**Character:** A single workhorse system stack throughout — no webfont ships, zero font payload. Weight and size do the talking; there is no display serif, no mono accent, no typographic play.

### Hierarchy
- **Display** (800, 44px, tight): cover-tile monograms only — two-letter disc monograms over blue gradients.
- **Headline** (700, 28px, -0.01em tracking): view titles (`Games`, counts beside them at 18px regular).
- **Title** (700, 15px): drive names in the status strip; card titles at 600/14px with single-line ellipsis.
- **Body** (400, 14px, 1.45): everything operational — menu rows, notices, empty states, settings copy.
- **Label** (600, 11–12px): nav-rail captions, pills, small status lines, platform corner tags (10px, uppercase, 0.06em tracking).

### Named Rules
**The Ellipsis Rule.** Titles truncate to one line with ellipsis; full titles live in `title` attributes, never in wrapped second lines.

## Layout

A fixed 96px icon rail on the left (sticky, full height, Bench Navy, hairline right border) plus a fluid main column capped at 1500px with generous bench padding (20px top, 36px sides, 60px bottom). Every view opens with a headline row, then a toolbar band (search well, filters, view toggle), then content.

The drive-status strip rides directly under the main padding on every view: drive icon, drive name, path, free-space readout, and a 120px capacity meter. Library content lays out as an auto-filling card grid (190px minimum tiles, 16px gutters) with a list-mode fallback (72px tile thumb, full-width rows, 10px rhythm). Spacing rhythm runs 4 / 8 / 10 / 12 / 16 / 20, with 36px as the page-side measure. The layout is fluid, not breakpoint-driven: the grid reflows by container width.

## Elevation & Depth

Depth is tonal, not cast: surfaces step up Abyss → Bench → Tray → Lift, and hover always means exactly one step lighter. There are no ambient shadows, no glows-as-structure, no blurred backdrops.

### Shadow Vocabulary
- **Menu Drop** (`box-shadow: 0 8px 28px rgba(0, 0, 0, 0.45)`): floating context menus only (game-card action menu). This is the system's sole shadow.

### Named Rules
**The Tonal Depth Rule.** If an element needs to sit above its neighbor, move it one tonal step up — never reach for a new shadow.

## Shapes

Round and confident: cards and the drivebar at a gentle 12px, buttons and fields at 10px, small danger actions and selects at 8px, pills and progress at full capsule (999px). The logo is a 14px rounded square flooded with Cathode Glow; the rail's nav targets are 12px soft squares with 11px captions beneath. Borders are always 1px Seam Blue on flat fills — no double borders, no gradient strokes, no clipping tricks. The signature silhouette is the game tile: a square (1:1 in grid, 72px thumb in list) with a radial blue gradient and a corner platform tag.

## Components

### Buttons
Tactile and confident. **Shape:** chunky radii (10px standard, 8px small). **Primary:** Cathode Glow fill, near-black 600-weight label, 10px 18px padding, no border; hover brightens the fill. **Ghost:** Tray fill, 1px Seam border, Dial Gray label, 8px 14px; hover lifts to Tray Lift with Console White text. **Danger ghost:** borderless-stroke Seam, Fault Coral text, 6px 12px at 8px radius.

### Pills
- **Style:** 12px 600-weight labels in full capsules (4px 10px), always a soft wash background with a saturated text color: mint wash for done/found, coral wash for error, blue wash for queued/filter states, Tray Lift with Dial Gray for neutral.
- **State:** status is carried by the wash+text pairing, never by icons alone; warning states use the amber wash.

### Cards / Containers
- **Corner Style:** gently rounded (12px).
- **Background:** Work Tray on Abyss ground.
- **Shadow Strategy:** none at rest (see Elevation & Depth).
- **Border:** 1px Seam Blue, always.
- **Internal Padding:** 10–12px for game cards, 12px rows, 28px centered for empty states.

### Inputs / Fields
- **Style:** Tray fill, 1px Seam stroke, 10px radius, full width, 10px 14px padding; toolbar variants (search well, selects, view toggle) share the same stroke-and-radius language.
- **Focus:** a 2px Signal-Edge outline pulled 1px inside (`outline-offset: -1px`); search wells shift border color to Signal Edge instead.
- **Error / Disabled:** errors surface as Fault Coral text or coral-wash notices beside the field, never as red strokes; disabled menu actions dim to 50% opacity.

### Navigation
Rail-first: 76px-wide stacked targets (20px stroke icons over 11px captions), Dial Gray at rest, Tray Lift wash on hover, blue wash + Console White when active. Settings parks at the rail foot beside a 10px API-status lamp (Faint Trace connecting, Confirm Mint connected, Fault Coral unreachable). No top bar, no breadcrumbs, no tabs.

### Progress
8px capsule track in Tray Lift with a Cathode Glow fill that eases width over 0.25s; the drive-capacity meter is the same language at 6px with a hairline Seam border on a Bench bed.

### Game Tile (Signature Component)
Box-proportioned cover tile (135:190, the PS2 keep-case front) with staged cover art filling edge to edge (`object-fit: cover`); titles without staged art fall back to a radial blue gradient (hue pinned per title to 200–230°) with a 44px 800-weight monogram. Platform tag (10px uppercase capsule on 55% black) pinned top-left in both states; covers load lazily and any load failure drops back to the monogram. List mode compresses to a 76px box-ratio thumb with a 24px monogram and no tag.

## Do's and Don'ts

Concrete guardrails grounded in the incumbent implementation.

### Do:
- **Do** stay dark-only — every new surface assumes Abyss ground and the four-step tonal scale.
- **Do** signal state with the soft-wash pill pairings (mint/coral/blue/amber wash + saturated text).
- **Do** lift hover exactly one tonal step (Tray → Lift, Bench hover → Lift).
- **Do** keep titles to one truncated line with the full text in a tooltip.
- **Do** reserve the single deep shadow for floating menus.

### Don't:
- **Don't** design or offer a light theme — the app pins dark and ships no light tokens.
- **Don't** introduce a second action hue alongside Cathode Glow Blue.
- **Don't** mint new badge colors outside the wash scale — new pills reuse the global soft-tint pairings.
- **Don't** cast shadows on cards, bars, or panels to imply hierarchy; step the tone instead.
- **Don't** wrap card titles, stretch monogram tiles out of square, or move the platform tag off the top-left.
