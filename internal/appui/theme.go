package appui

import (
	"formigo/internal/converter"

	"github.com/egoist/mygo/ui"
)

// CategoryColors holds foreground and subtle badge background colors for a file category.
type CategoryColors struct {
	FG     ui.Color
	SoftBG ui.Color
	Border ui.Color
}

// SurfacePalette provides multi-elevation native desktop surface tokens inspired by
// mygo-music, AudioSmith, and MyGO-Clash so chrome, workspace sheets, cards, and sunken
// wells maintain crisp hairline separation without heavy box-soup.
type SurfacePalette struct {
	ShellBG         ui.Color // Outer window frame / non-vibrancy sidebar backdrop
	Canvas          ui.Color // Inset rounded main workspace sheet background
	HeaderBG        ui.Color // Top workspace toolbar surface
	SubBarBG        ui.Color // Secondary filter & batch utility strip
	Card            ui.Color // Elevated queue row / engine card / settings group
	CardHover       ui.Color // Hovered card surface
	CardBorder      ui.Color // Resting 1px hairline border on cards
	CardBorderHover ui.Color // Hovered 1px hairline border on cards
	Subtle          ui.Color // Sunken well (dashed drop zone, format capsule, code chip)
	SubtleBorder    ui.Color // Hairline on sunken wells
	ShellBorder     ui.Color // Outer border of the inset workspace sheet
	Divider         ui.Color // Subtle section/row hairline divider
	NavActiveBG     ui.Color // Sliding active pill in sidebar
	NavActiveBorder ui.Color // Subtle border on active sidebar pill
	NavHoverBG      ui.Color // Hovered sidebar item fill
	TextSecondary   ui.Color // High-legibility secondary body text (~80% contrast)
	TextFaint       ui.Color // Subtle hints, shortcuts, and inactive badges
	SheetShadow     ui.Color // Soft ambient shadow behind the inset workspace sheet
	CardShadow      ui.Color // Ultra-subtle resting card shadow
	TopHighlight    ui.Color // Specular 1px top inset highlight in dark mode
}

// PaletteFor returns the multi-elevation surface tokens matching the active theme.
func PaletteFor(t *ui.Theme) SurfacePalette {
	if t.Dark {
		return SurfacePalette{
			ShellBG:         ui.Hex("#090E1A"),
			Canvas:          ui.Hex("#0E1526"),
			HeaderBG:        ui.Hex("#10192D"),
			SubBarBG:        ui.Hex("#0C1322"),
			Card:            ui.Hex("#141E33"),
			CardHover:       ui.Hex("#18253F"),
			CardBorder:      ui.Hex("#21304D"),
			CardBorderHover: ui.Hex("#314770"),
			Subtle:          ui.Hex("#0A101D"),
			SubtleBorder:    ui.Hex("#1C2A44"),
			ShellBorder:     ui.Hex("#1E2C47"),
			Divider:         ui.Hex("#1B2840"),
			NavActiveBG:     ui.Hex("#17243E"),
			NavActiveBorder: ui.Hex("#283C62"),
			NavHoverBG:      ui.RGBA(255, 255, 255, 0.045),
			TextSecondary:   ui.Hex("#CBD5E1"),
			TextFaint:       ui.Hex("#64748B"),
			SheetShadow:     ui.RGBA(0, 0, 0, 0.42),
			CardShadow:      ui.RGBA(0, 0, 0, 0.22),
			TopHighlight:    ui.RGBA(255, 255, 255, 0.055),
		}
	}
	return SurfacePalette{
		ShellBG:         ui.Hex("#EAEFF7"),
		Canvas:          ui.Hex("#F8FAFC"),
		HeaderBG:        ui.Hex("#FFFFFF"),
		SubBarBG:        ui.Hex("#F3F6FB"),
		Card:            ui.Hex("#FFFFFF"),
		CardHover:       ui.Hex("#FFFFFF"),
		CardBorder:      ui.Hex("#E2E8F0"),
		CardBorderHover: ui.Hex("#CBD5E1"),
		Subtle:          ui.Hex("#F1F5F9"),
		SubtleBorder:    ui.Hex("#DCE4EE"),
		ShellBorder:     ui.Hex("#D6E0EE"),
		Divider:         ui.Hex("#E6ECF4"),
		NavActiveBG:     ui.Hex("#FFFFFF"),
		NavActiveBorder: ui.Hex("#DCE4F0"),
		NavHoverBG:      ui.RGBA(15, 23, 42, 0.045),
		TextSecondary:   ui.Hex("#334155"),
		TextFaint:       ui.Hex("#94A3B8"),
		SheetShadow:     ui.RGBA(15, 23, 42, 0.065),
		CardShadow:      ui.RGBA(15, 23, 42, 0.035),
		TopHighlight:    ui.RGBA(255, 255, 255, 0.80),
	}
}

// applyAppearance configures Fomigo's Gopher Sky-Blue & Deep-Navy brand theme.
// Importantly, WithPreferences is applied BEFORE setting brand colors so OS wallpaper
// dynamic colors never overwrite Fomigo's brand palette.
func (s *AppState) applyAppearance(c *ui.Context) {
	prefs := c.Preferences()
	dark := false
	switch s.PrefAppearanceIdx {
	case 1:
		dark = false
	case 2:
		dark = true
	default:
		dark = c.Theme().Dark
	}

	if dark {
		c.SetTheme(buildBrandDarkTheme(prefs))
	} else {
		c.SetTheme(buildBrandLightTheme(prefs))
	}
}

func buildBrandLightTheme(prefs ui.Preferences) *ui.Theme {
	th := ui.LightTheme().WithPreferences(prefs)
	th.Dark = false
	// Keep Background pure white and Surface subtly sunken so native Buttons, Selects,
	// TextInputs, and Segmented active thumbs render with crisp macOS elevation contrast.
	th.Background = ui.Hex("#FFFFFF")
	th.Surface = ui.Hex("#F1F5F9")
	th.SurfaceHover = ui.Hex("#E7EDF5")
	th.SurfacePressed = ui.Hex("#DCE5F2")
	th.Border = ui.Hex("#D5DFEC")
	th.Text = ui.Hex("#0F172A")
	th.TextMuted = ui.Hex("#64748B")
	th.Accent = ui.Hex("#1D6FF2")
	th.AccentHover = ui.Hex("#165DD0")
	th.AccentPressed = ui.Hex("#114CAE")
	th.AccentText = ui.Hex("#FFFFFF")
	th.InactiveAccent = ui.Hex("#1D6FF2")
	th.Focus = ui.RGBA(29, 111, 242, 0.50)
	th.Selection = ui.RGBA(29, 111, 242, 0.18)
	th.Success = ui.Hex("#0D9F6E")
	th.Warning = ui.Hex("#D97706")
	th.Danger = ui.Hex("#E11D48")
	th.Radius = 6
	th.ScrollbarWidth = 6
	return th
}

func buildBrandDarkTheme(prefs ui.Preferences) *ui.Theme {
	th := ui.DarkTheme().WithPreferences(prefs)
	th.Dark = true
	th.Background = ui.Hex("#0E1526")
	th.Surface = ui.Hex("#1B2740")
	th.SurfaceHover = ui.Hex("#243352")
	th.SurfacePressed = ui.Hex("#2E4066")
	th.Border = ui.Hex("#2A3B5C")
	th.Text = ui.Hex("#F1F5F9")
	th.TextMuted = ui.Hex("#8B9BB4")
	th.Accent = ui.Hex("#3B82F6")
	th.AccentHover = ui.Hex("#60A5FA")
	th.AccentPressed = ui.Hex("#2563EB")
	th.AccentText = ui.Hex("#FFFFFF")
	th.InactiveAccent = ui.Hex("#3B82F6")
	th.Focus = ui.RGBA(96, 165, 250, 0.55)
	th.Selection = ui.RGBA(59, 130, 246, 0.28)
	th.Success = ui.Hex("#10B981")
	th.Warning = ui.Hex("#F59E0B")
	th.Danger = ui.Hex("#F43F5E")
	th.Radius = 6
	th.ScrollbarWidth = 6
	return th
}

// CategoryPalette returns harmonized accent colors derived from the Gopher logo's floating file cards.
func CategoryPalette(cat converter.Category, dark bool) CategoryColors {
	var fg ui.Color
	var lightBG string
	switch cat {
	case converter.CategoryImage:
		// Periwinkle Lavender (left floating image card in logo)
		fg = ui.Hex("#686BF0")
		if dark {
			fg = ui.Hex("#8B8EFF")
		}
		lightBG = "#EEF0FF"
	case converter.CategoryVideo, converter.CategoryAudio:
		// Mint Teal (right floating media card in logo)
		fg = ui.Hex("#18A38E")
		if dark {
			fg = ui.Hex("#3DD4BC")
		}
		lightBG = "#E4F8F4"
	case converter.CategoryPDF:
		// Crisp Coral Rose
		fg = ui.Hex("#E54865")
		if dark {
			fg = ui.Hex("#FF758F")
		}
		lightBG = "#FFF0F3"
	case converter.CategorySpreadsheet, converter.CategoryData:
		// Fresh Emerald
		fg = ui.Hex("#129E66")
		if dark {
			fg = ui.Hex("#34D399")
		}
		lightBG = "#E6F8F0"
	case converter.CategoryArchive, converter.CategorySubtitle:
		// Warm Amber
		fg = ui.Hex("#D98218")
		if dark {
			fg = ui.Hex("#FBBF24")
		}
		lightBG = "#FFF6E6"
	default:
		// Electric Sky Blue (center document in logo)
		fg = ui.Hex("#1D75F8")
		if dark {
			fg = ui.Hex("#58A6FF")
		}
		lightBG = "#EAF3FF"
	}

	if dark {
		return CategoryColors{
			FG:     fg,
			SoftBG: fg.Alpha(0.16),
			Border: fg.Alpha(0.32),
		}
	}
	return CategoryColors{
		FG:     fg,
		SoftBG: ui.Hex(lightBG),
		Border: fg.Alpha(0.24),
	}
}

func cardShadowColor(dark bool) ui.Color {
	if dark {
		return ui.RGBA(0, 0, 0, 0.22)
	}
	return ui.RGBA(15, 23, 42, 0.035)
}
