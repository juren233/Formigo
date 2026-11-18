package appui

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"formigo/internal/config"
	"formigo/internal/converter"
	"formigo/internal/i18n"

	"github.com/egoist/mygo/plugins/loading"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/mobile"
)

type navPillMatch struct{}

// View renders the Fomigo native GPU interface, adapting responsively between desktop and mobile viewports.
func (s *AppState) View(c *ui.Context) {
	s.applyAppearance(c)
	t := c.Theme()
	pal := PaletteFor(t)
	lang := s.ResolvedLang()
	cat := i18n.T(lang)

	if s.PendingToast != "" {
		msg := s.PendingToast
		rev := s.PendingToastReveal
		s.PendingToast = ""
		s.PendingToastReveal = ""
		if rev != "" && s.Bridge.RevealPath != nil && !s.Bridge.IsMobile {
			c.ToastAction(msg, cat.ActionReveal, func() {
				s.Bridge.RevealPath(rev)
			})
		} else {
			c.Toast(msg)
		}
	}

	winW, _ := c.Size()
	compact := (winW > 0 && winW < 720) || (s.Bridge.IsMobile && winW < 840)

	if compact {
		s.renderCompactShell(c, cat, lang, t, pal)
		return
	}

	s.handleDesktopShortcuts(c)

	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}

	root := ui.Row(c).Fill()
	if !c.Vibrancy() {
		root.Background(pal.ShellBG)
	}
	if dropped := root.DroppedFiles(); len(dropped) > 0 {
		s.AddPaths(dropped)
	}

	root.Children(func() {
		// Translucent Vibrancy Sidebar Rail
		s.renderSidebar(c, cat, lang, t, pal)

		// Inset Concentric Main Workspace Sheet (mygo-music / AudioSmith / Arc style)
		sheet := ui.Column(c.Key("workspace-sheet")).Grow(1).FillHeight().
			Margin(8, 8, 8, 0).Radius(12).
			Background(pal.Canvas).Border(1, pal.ShellBorder).
			Shadow(0, 2, 12, 0, pal.SheetShadow).Clip()
		if t.Dark {
			sheet.InsetShadow(0, 1, 0, 0, pal.TopHighlight)
		}

		sheet.Children(func() {
			switch s.ActivePage {
			case PageMerge:
				s.renderMergePage(c, cat, lang, t, pal, false)
			case PageEngines:
				s.renderEnginesPage(c, cat, lang, t, pal, false)
			case PageSettings:
				s.renderSettingsPage(c, cat, lang, t, pal, false)
			default:
				s.renderConvertPage(c, cat, lang, t, pal, false)
			}
		})
	})
}

func (s *AppState) handleDesktopShortcuts(c *ui.Context) {
	if c.Shortcut(ui.Cmd, ui.Key1) {
		s.ActivePage = PageConvert
	}
	if c.Shortcut(ui.Cmd, ui.Key2) {
		s.ActivePage = PageMerge
	}
	if c.Shortcut(ui.Cmd, ui.Key3) {
		s.ActivePage = PageEngines
	}
	if c.Shortcut(ui.Cmd, ui.Key4) {
		s.ActivePage = PageSettings
	}
	if c.Shortcut(ui.Cmd, ui.KeyI) && s.ActivePage == PageConvert {
		s.InspectorCollapsed = !s.InspectorCollapsed
	}
	if c.Shortcut(ui.Cmd, ui.KeyO) && s.Bridge.PickFiles != nil {
		lastDir := s.Store.Get().LastOpenDir
		if s.ActivePage == PageMerge {
			s.Bridge.PickFiles(lastDir, func(paths []string) { s.AddMergeFiles(paths) })
		} else {
			s.ActivePage = PageConvert
			s.Bridge.PickFiles(lastDir, func(paths []string) { s.AddQueueFiles(paths) })
		}
	}
	if c.Shortcut(ui.Cmd, ui.KeyEnter) {
		if s.ActivePage == PageConvert && !s.BatchRunning && len(s.Items) > 0 {
			s.StartAll()
		}
	}
}

func renderBrandWordmark(c *ui.Context, t *ui.Theme, size float32) {
	ui.RichText(c,
		ui.Span{Text: "Fom", Size: size, Weight: 800, Color: t.Text},
		ui.Span{Text: "igo", Size: size, Weight: 800, Color: t.Accent},
	).SingleLine()
}

// --- Mobile / Compact Shell ---

func (s *AppState) renderCompactShell(c *ui.Context, cat i18n.Catalog, lang i18n.Lang, t *ui.Theme, pal SurfacePalette) {
	root := ui.Column(c.Key("mobile-shell")).Fill().Background(pal.Canvas)
	if dropped := root.DroppedFiles(); len(dropped) > 0 {
		s.AddPaths(dropped)
	}

	root.Children(func() {
		s.renderMobileHeader(c, cat, t, pal)
		ui.Box(c).FillWidth().Height(1).Background(pal.Divider)

		ui.Column(c.Key("mobile-body")).Grow(1).FillWidth().Clip().Children(func() {
			switch s.ActivePage {
			case PageMerge:
				s.renderMergePage(c, cat, lang, t, pal, true)
			case PageEngines:
				s.renderEnginesPage(c, cat, lang, t, pal, true)
			case PageSettings:
				s.renderSettingsPage(c, cat, lang, t, pal, true)
			default:
				s.renderConvertPage(c, cat, lang, t, pal, true)
			}
		})

		ui.Box(c).FillWidth().Height(1).Background(pal.Divider)
		s.renderMobileBottomNav(c, cat, t, pal)
	})

	mobile.Sheet(c, &s.MobileOptionsOpen, mobile.SheetOptions{
		Detents: []float32{0.68, 0.92},
		Label:   cat.InspectorTitle,
	}, func() {
		ui.Scroll(c.Key("mobile-opt-sheet")).Fill().Padding(12, 18, 24, 18).Gap(14).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, cat.InspectorTitle).FontSize(16).Bold().Grow(1)
				if ui.Button(c.Key("btn-close-sheet"), "✓").Clicked() {
					s.MobileOptionsOpen = false
				}
			})
			ui.Divider(c)
			s.renderOptionsControls(c, cat, t, pal)
		})
	})
}

func (s *AppState) renderMobileHeader(c *ui.Context, cat i18n.Catalog, t *ui.Theme, pal SurfacePalette) {
	top, _, _, _ := c.SafeArea()
	ui.Row(c.Key("mobile-header")).FillWidth().Height(54+top).Padding(top, 14, 0, 14).Margin(-top, 0, 0, 0).Shrink(0).Gap(10).AlignItems(ui.Center).Background(pal.HeaderBG).Children(func() {
		ui.Box(c).Size(34, 34).Radius(9).Clip().Border(1, pal.CardBorder).Children(func() {
			ui.Image(c, AvatarBitmap).Size(34, 34)
		})

		ui.Row(c).Gap(8).AlignItems(ui.Center).Grow(1).Children(func() {
			renderBrandWordmark(c, t, 17.5)
			if len(s.Items) > 0 && s.ActivePage == PageConvert {
				ui.Row(c).Padding(2, 8).Radius(99).Background(t.Accent.Alpha(0.12)).Children(func() {
					ui.Textf(c, "%d", len(s.Items)).FontSize(11).FontWeight(700).FontFeatures("tnum").TextColor(t.Accent).SingleLine()
				})
			}
		})

		if s.ActivePage == PageConvert {
			optBtn := ui.Button(c.Key("btn-mobile-opts"), "").Padding(6, 10)
			optBtn.Children(func() {
				ui.Icon(c, IconSliders).FontSize(13.5).TextColor(t.Accent)
				ui.Text(c, cat.MobileOptionsBtn).FontSize(12).FontWeight(600).SingleLine()
			})
			if optBtn.Clicked() {
				s.MobileOptionsOpen = true
			}
		}
	})
}

func (s *AppState) renderMobileBottomNav(c *ui.Context, cat i18n.Catalog, t *ui.Theme, pal SurfacePalette) {
	_, _, bottom, _ := c.SafeArea()
	if bottom > 120 {
		return
	}

	type navTab struct {
		page  string
		label string
		icon  *ui.SVG
		badge int
	}
	tabs := []navTab{
		{PageConvert, cat.NavConvert, IconBolt, len(s.Items)},
		{PageMerge, cat.NavMerge, IconLayers, len(s.MergeItems)},
		{PageEngines, cat.NavEngines, IconCPU, 0},
		{PageSettings, cat.NavSettings, IconSettings, 0},
	}

	bar := ui.Row(c.Key("mobile-bottom-nav")).FillWidth().Background(pal.HeaderBG).
		Padding(5, 6, 5+bottom, 6).Margin(0, 0, -bottom, 0).Shrink(0).Role(ui.RoleTabList)

	bar.Children(func() {
		for _, tb := range tabs {
			tab := tb
			active := s.ActivePage == tab.page
			fg := t.TextMuted
			if active {
				fg = t.Accent
			}
			item := ui.Column(c.Key("mtab-"+tab.page)).Grow(1).Basis(0).Height(54).
				AlignItems(ui.Center).Justify(ui.Center).Gap(3).
				Role(ui.RoleTab).Label(tab.label).Checked(active)

			item.Children(func() {
				pill := ui.Row(c).Size(54, 28).Radius(14).Center().Gap(4)
				if active {
					pill.Background(t.Accent.Alpha(0.13))
				}
				pill.Children(func() {
					ui.Icon(c, tab.icon).FontSize(17).TextColor(fg)
					if tab.badge > 0 {
						ui.Textf(c, "%d", tab.badge).FontSize(10.5).Bold().FontFeatures("tnum").TextColor(fg).SingleLine()
					}
				})
				weight := 500
				if active {
					weight = 700
				}
				ui.Text(c, tab.label).FontSize(11).FontWeight(weight).TextColor(fg).SingleLine()
			})
			if item.Clicked() {
				s.ActivePage = tab.page
			}
		}
	})
}

// --- Desktop Translucent Sidebar Rail ---

func (s *AppState) renderSidebar(c *ui.Context, cat i18n.Catalog, lang i18n.Lang, t *ui.Theme, pal SurfacePalette) {
	tb := c.TitleBar()
	topPad := float32(16)
	if tb.Bottom > 0 {
		topPad = max(topPad, tb.Bottom+12)
	} else if runtime.GOOS == "darwin" && !s.Bridge.IsMobile {
		// Reserve clean room for inset macOS traffic lights even in headless snapshots
		topPad = 42
	}

	rail := ui.Column(c.Key("sidebar-col")).Width(214).FillHeight().
		Padding(topPad, 12, 12, 12).Gap(14).DragWindow()

	rail.Children(func() {
		// Unboxed Native Studio Brand Header
		ui.Row(c.Key("sidebar-brand")).FillWidth().Padding(2, 6).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(36, 36).Radius(9).Clip().
				Border(1, pal.CardBorder).Shadow(0, 2, 6, 0, pal.CardShadow).Children(func() {
				ui.Image(c, AvatarBitmap).Size(36, 36)
			})
			ui.Column(c).Gap(1).Grow(1).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					renderBrandWordmark(c, t, 17)
					tag := "LOCAL"
					if lang == i18n.LangZH {
						tag = "离线"
					}
					ui.Row(c).Padding(1, 5).Radius(4).Background(t.Accent.Alpha(0.12)).Children(func() {
						ui.Text(c, tag).FontSize(9.5).FontWeight(700).TextColor(t.Accent).SingleLine()
					})
				})
				sub := "Pure Go · GPU Studio"
				if lang == i18n.LangZH {
					sub = "极速多格式转换工坊"
				}
				ui.Text(c, sub).FontSize(11).TextColor(t.TextMuted).SingleLine()
			})
		})

		// Grouped Custom Navigation with Spring-Gliding Pill Indicator
		ui.Column(c.Key("sidebar-nav")).Grow(1).FillWidth().Gap(14).Children(func() {
			ui.Column(c).FillWidth().Gap(3).Children(func() {
				ui.Row(c).Padding(0, 8, 2, 8).Children(func() {
					ui.Text(c, cat.NavGroupWork).FontSize(10.5).FontWeight(700).TextColor(pal.TextFaint).SingleLine()
				})
				s.renderSidebarItem(c, PageConvert, IconBolt, cat.NavConvert, len(s.Items), t, pal)
				s.renderSidebarItem(c, PageMerge, IconLayers, cat.NavMerge, len(s.MergeItems), t, pal)
			})

			ui.Column(c).FillWidth().Gap(3).Children(func() {
				ui.Row(c).Padding(0, 8, 2, 8).Children(func() {
					ui.Text(c, cat.NavGroupSystem).FontSize(10.5).FontWeight(700).TextColor(pal.TextFaint).SingleLine()
				})
				s.renderSidebarItem(c, PageEngines, IconCPU, cat.NavEngines, 0, t, pal)
				s.renderSidebarItem(c, PageSettings, IconSettings, cat.NavSettings, 0, t, pal)
			})
		})

		// Interactive Live Engine & Batch Activity Footer Card
		readyCount := 0
		for _, st := range s.EngineStatuses {
			if st.Available {
				readyCount++
			}
		}
		totalFormats := s.countUniqueFormats()
		_, activeCnt, doneCnt, _, overall := s.QueueCounts()

		footer := ui.Column(c.Key("sidebar-footer")).FillWidth().Padding(10, 11).Radius(10).
			Background(pal.NavActiveBG.Alpha(0.75)).Border(1, pal.NavActiveBorder).
			Gap(5).Cursor(ui.CursorPointer)
		if footer.Hovered() {
			footer.Background(pal.NavActiveBG)
		}
		if footer.Clicked() {
			if s.BatchRunning {
				s.ActivePage = PageConvert
			} else {
				s.ActivePage = PageEngines
			}
		}

		footer.Children(func() {
			if s.BatchRunning {
				ui.Row(c).FillWidth().Gap(7).AlignItems(ui.Center).Children(func() {
					loading.Ring(c, loading.Options{Size: 13, Color: t.Accent})
					lbl := fmt.Sprintf("正在转换 %d 项", activeCnt)
					if lang == i18n.LangEN {
						lbl = fmt.Sprintf("Converting %d", activeCnt)
					}
					ui.Text(c, lbl).FontSize(11.5).FontWeight(600).FontFeatures("tnum").Grow(1).SingleLine()
					ui.Textf(c, "%d/%d", doneCnt, len(s.Items)).FontSize(11).FontWeight(700).FontFeatures("tnum").TextColor(t.Accent).SingleLine()
				})
				ui.Progress(c.Key("sidebar-prog"), overall).Label(cat.StageConverting)
			} else {
				ui.Row(c).FillWidth().Gap(7).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(14, 14).Radius(7).Background(t.Success.Alpha(0.18)).Center().Children(func() {
						ui.Box(c).Size(6, 6).Radius(3).Background(t.Success)
					})
					ui.Textf(c, "%d / %d %s", readyCount, len(s.EngineStatuses), cat.EngineAvailable).
						FontSize(11.5).FontWeight(600).FontFeatures("tnum").TextColor(t.Text).Grow(1).SingleLine()
				})
				formatHint := fmt.Sprintf("%d formats ready offline", totalFormats)
				if lang == i18n.LangZH {
					formatHint = fmt.Sprintf("已就绪 %d 种格式本地互转", totalFormats)
				}
				ui.Text(c, formatHint).FontSize(10.5).FontFeatures("tnum").TextColor(t.TextMuted).SingleLine()
			}
		})
	})
}

func (s *AppState) renderSidebarItem(c *ui.Context, page string, icon *ui.SVG, label string, badge int, t *ui.Theme, pal SurfacePalette) {
	active := s.ActivePage == page
	item := ui.Box(c.Key("nav-" + page)).FillWidth().Height(34).Radius(8).
		Cursor(ui.CursorPointer).Focusable().Role(ui.RoleTab).Label(label).Checked(active)

	if !active && item.Hovered() {
		item.Background(pal.NavHoverBG)
	}
	if item.Clicked() {
		s.ActivePage = page
	}

	item.Children(func() {
		if active {
			pill := ui.Box(c).Absolute().Fill().Radius(8).
				Background(pal.NavActiveBG).Border(1, pal.NavActiveBorder).
				Shadow(0, 1, 3, 0, pal.CardShadow).
				Transition(ui.ElementTransition{Match: navPillMatch{}, Animation: &ui.Snappy})
			if t.Dark {
				pill.InsetShadow(0, 1, 0, 0, pal.TopHighlight)
			}
		}

		fg := pal.TextSecondary
		iconCol := t.TextMuted
		weight := 500
		if active {
			fg = t.Text
			iconCol = t.Accent
			weight = 600
		}

		ui.Row(c).Fill().Padding(0, 10).Gap(9).AlignItems(ui.Center).ZIndex(1).Children(func() {
			ui.Icon(c, icon).FontSize(15.5).TextColor(iconCol)
			ui.Text(c, label).FontSize(13).FontWeight(weight).TextColor(fg).Grow(1).SingleLine()
			if badge > 0 {
				badgeBG := pal.Subtle
				badgeFG := t.TextMuted
				if active {
					badgeBG = t.Accent.Alpha(0.14)
					badgeFG = t.Accent
				}
				ui.Row(c).Padding(1, 7).Radius(99).Background(badgeBG).Children(func() {
					ui.Textf(c, "%d", badge).FontSize(11).FontWeight(700).FontFeatures("tnum").
						TextColor(badgeFG).ContentTransition(ui.ContentTransition{Numeric: true}).SingleLine()
				})
			}
		})
	})
}

func (s *AppState) countUniqueFormats() int {
	seen := make(map[string]bool)
	for _, g := range s.FormatMatrix {
		for _, ext := range g.Sources {
			seen[ext] = true
		}
		for _, ext := range g.Targets {
			seen[ext] = true
		}
	}
	return len(seen)
}

// --- Shared Desktop Workspace Header ---

func renderWorkspaceHeader(c *ui.Context, key, title, pillText string, t *ui.Theme, pal SurfacePalette, actions func()) {
	tb := c.TitleBar()
	rightPad := float32(16)
	if tb.Right > 0 {
		rightPad = max(rightPad, tb.Right+12)
	}

	ui.Column(c.Key(key)).FillWidth().Shrink(0).Background(pal.HeaderBG).Children(func() {
		ui.Row(c).FillWidth().Height(50).Padding(0, rightPad, 0, 18).Gap(10).
			AlignItems(ui.Center).DragWindow().Children(func() {
			ui.Text(c, title).FontSize(15.5).Bold().SingleLine()
			if pillText != "" {
				ui.Row(c).Padding(2, 8).Radius(99).Background(pal.Subtle).Border(1, pal.SubtleBorder).Children(func() {
					ui.Text(c, pillText).FontSize(11).FontWeight(600).FontFeatures("tnum").TextColor(t.TextMuted).SingleLine()
				})
			}
			ui.Spacer(c)
			if actions != nil {
				ui.Row(c).Gap(7).AlignItems(ui.Center).Children(actions)
			}
		})
		ui.Box(c).FillWidth().Height(1).Background(pal.Divider)
	})
}

// --- Workspace 1: Batch Convert ---

func (s *AppState) renderConvertPage(c *ui.Context, cat i18n.Catalog, lang i18n.Lang, t *ui.Theme, pal SurfacePalette, compact bool) {
	if compact {
		s.renderConvertPageCompact(c, cat, lang, t, pal)
		return
	}

	pendingCnt, activeCnt, doneCnt, failedCnt, _ := s.QueueCounts()
	var totalBytes int64
	for _, it := range s.Items {
		totalBytes += it.SizeBytes
	}
	summaryPill := ""
	if len(s.Items) > 0 {
		if lang == i18n.LangEN {
			summaryPill = fmt.Sprintf("%d items · %s", len(s.Items), HumanBytes(totalBytes))
		} else {
			summaryPill = fmt.Sprintf("%d 项 · %s", len(s.Items), HumanBytes(totalBytes))
		}
	}

	ui.Column(c.Key("page-convert")).Fill().Clip().Children(func() {
		// 1. Unified Top Workspace Toolbar
		renderWorkspaceHeader(c, "convert-header", cat.NavConvert, summaryPill, t, pal, func() {
			addBtn := ui.Button(c.Key("btn-add-files"), "").Tooltip(cat.AddFiles + " (⌘O)")
			addBtn.Children(func() {
				ui.Icon(c, IconPlus).FontSize(14).TextColor(t.Accent)
				ui.Text(c, cat.AddFiles).FontWeight(600).SingleLine()
			})
			if addBtn.Clicked() && s.Bridge.PickFiles != nil {
				lastDir := s.Store.Get().LastOpenDir
				s.Bridge.PickFiles(lastDir, func(paths []string) { s.AddQueueFiles(paths) })
			}

			if !s.Bridge.IsMobile && s.Bridge.PickFolder != nil {
				folderBtn := ui.Button(c.Key("btn-add-folder"), "").Tooltip(cat.AddFolder)
				folderBtn.Children(func() {
					ui.Icon(c, IconFolder).FontSize(14).TextColor(t.TextMuted)
					ui.Text(c, cat.AddFolder).SingleLine()
				})
				if folderBtn.Clicked() {
					lastDir := s.Store.Get().LastOpenDir
					s.Bridge.PickFolder(lastDir, func(dir string) {
						if dir != "" {
							s.AddPaths([]string{dir})
						}
					})
				}
			}

			if len(s.Items) > 0 {
				if doneCnt > 0 {
					if ui.Button(c.Key("btn-clear-done"), cat.ClearCompleted).Disabled(s.BatchRunning).Clicked() {
						s.ClearCompleted()
					}
				}
				clearBtn := ui.Button(c.Key("btn-clear-all"), "").Label(cat.ClearAll).Tooltip(cat.ClearAll).Disabled(s.BatchRunning)
				clearBtn.Children(func() {
					ui.Icon(c, IconTrash).FontSize(13.5).TextColor(t.TextMuted)
					ui.Text(c, cat.ClearAll).SingleLine()
				})
				if clearBtn.Clicked() {
					s.ClearAll()
				}
			}

			// Inspector Collapse/Expand Toggle (⌘I)
			inspToggle := ui.Button(c.Key("btn-toggle-inspector"), "").
				Label(cat.InspectorTitle).Tooltip(cat.InspectorTitle + " (⌘I)")
			if !s.InspectorCollapsed {
				inspToggle.Background(t.Accent.Alpha(0.11)).Border(1, t.Accent.Alpha(0.28))
			}
			inspToggle.Children(func() {
				ic := t.TextMuted
				if !s.InspectorCollapsed {
					ic = t.Accent
				}
				ui.Icon(c, IconSliders).FontSize(14).TextColor(ic)
			})
			if inspToggle.Clicked() {
				s.InspectorCollapsed = !s.InspectorCollapsed
			}

			if s.BatchRunning {
				stopBtn := ui.PrimaryButton(c.Key("btn-stop-all"), "").Background(t.Danger)
				stopBtn.Children(func() {
					ui.Icon(c, IconStop).FontSize(13.5)
					ui.Text(c, cat.CancelAll).FontWeight(600).SingleLine()
				})
				if stopBtn.Clicked() {
					s.CancelAll()
				}
			} else {
				startBtn := ui.PrimaryButton(c.Key("btn-start-all"), "").
					Disabled(len(s.Items) == 0 || (pendingCnt == 0 && failedCnt == 0)).
					Tooltip(cat.StartAll + " (⌘↩)")
				startBtn.Children(func() {
					ui.Icon(c, IconPlay).FontSize(13.5)
					ui.Text(c, cat.StartAll).FontWeight(600).SingleLine()
				})
				if startBtn.Clicked() {
					s.StartAll()
				}
			}
		})

		// 2. Sleek Utility Strip (Filter Tabs + Batch Target + Output Mode — only when queue has items)
		if len(s.Items) > 0 {
			s.renderConvertUtilityBar(c, cat, t, pal, pendingCnt, activeCnt, doneCnt, failedCnt)
		}

		// 3. Main Split Workspace: Queue / Drop Well on Left + Docked Options Inspector on Right
		ui.Row(c.Key("convert-split")).Grow(1).FillWidth().Clip().Children(func() {
			ui.Column(c.Key("queue-column")).Grow(1).FillHeight().Clip().Children(func() {
				if len(s.Items) == 0 {
					ui.Column(c).Fill().Padding(16).Children(func() {
						s.renderDropZone(c, cat, lang, t, pal, true, false)
					})
				} else {
					ui.Column(c).Fill().Padding(12, 14, 14, 14).Gap(10).Clip().Children(func() {
						s.renderDropZone(c, cat, lang, t, pal, false, false)
						visible := s.FilteredItems()
						ui.Scroll(c.Key("queue-scroll")).Grow(1).FillWidth().Gap(7).Children(func() {
							for _, item := range visible {
								it := item
								s.renderQueueCard(c, it, cat, lang, t, pal, false)
							}
						})
					})
				}
			})

			if !s.InspectorCollapsed {
				ui.Box(c).FillHeight().Width(1).Background(pal.Divider)
				s.renderOptionsInspector(c, cat, t, pal)
			}
		})
	})
}

func (s *AppState) renderConvertUtilityBar(c *ui.Context, cat i18n.Catalog, t *ui.Theme, pal SurfacePalette, pendingCnt, activeCnt, doneCnt, failedCnt int) {
	ui.Column(c.Key("convert-util-wrap")).FillWidth().Shrink(0).Background(pal.SubBarBG).Children(func() {
		ui.Row(c.Key("convert-util-bar")).FillWidth().Padding(6, 14).Gap(10).Wrap().AlignItems(ui.Center).Children(func() {
			// Status Filter Pills
			type filterSpec struct {
				id    string
				label string
				count int
			}
			filters := []filterSpec{
				{"all", cat.FilterAll, len(s.Items)},
				{"pending", cat.FilterPending, pendingCnt},
				{"active", cat.FilterActive, activeCnt},
				{"done", cat.FilterDone, doneCnt},
				{"failed", cat.FilterFailed, failedCnt},
			}
			ui.Row(c.Key("filter-pills")).Gap(4).AlignItems(ui.Center).Children(func() {
				for _, fl := range filters {
					if fl.id != "all" && fl.count == 0 && s.QueueFilter != fl.id {
						continue
					}
					f := fl
					active := s.QueueFilter == f.id || (s.QueueFilter == "" && f.id == "all")
					pill := ui.Row(c.Key("flt-" + f.id)).Padding(4, 9).Radius(6).Gap(5).
						AlignItems(ui.Center).Cursor(ui.CursorPointer)
					if active {
						pill.Background(pal.Card).Border(1, pal.CardBorder).Shadow(0, 1, 2, 0, pal.CardShadow)
					} else if pill.Hovered() {
						pill.Background(pal.NavHoverBG)
					}
					if pill.Clicked() {
						s.QueueFilter = f.id
					}
					pill.Children(func() {
						fg := t.TextMuted
						w := 500
						if active {
							fg = t.Text
							w = 600
						}
						ui.Text(c, f.label).FontSize(11.5).FontWeight(w).TextColor(fg).SingleLine()
						ui.Textf(c, "%d", f.count).FontSize(11).FontWeight(700).FontFeatures("tnum").
							TextColor(t.Accent.Mix(fg, 0.4)).SingleLine()
					})
				}
			})

			ui.Spacer(c)

			// Batch Target Selector
			s.renderBatchTargetControls(c, cat, t)

			// Compact Output Destination Switcher
			if !s.Bridge.IsMobile {
				ui.Box(c).Size(1, 16).Background(pal.Divider)
				ui.Row(c.Key("inline-out-dir")).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, IconFolderOpen).FontSize(13.5).TextColor(t.TextMuted)
					if ui.Segmented(c.Key("seg-out-mode"), &s.OutputModeIdx, cat.SameAsSource, cat.CustomDir).Changed() {
						s.SavePreferences()
					}
					if s.OutputModeIdx == 1 {
						dirLabel := cat.ChooseDir
						if strings.TrimSpace(s.CustomOutputDir) != "" {
							dirLabel = filepath.Base(strings.TrimSpace(s.CustomOutputDir))
						}
						pickBtn := ui.Button(c.Key("btn-pick-outdir"), dirLabel).Tooltip(s.CustomOutputDir)
						if pickBtn.Clicked() && s.Bridge.PickFolder != nil {
							s.Bridge.PickFolder(s.CustomOutputDir, func(dir string) {
								if dir != "" {
									s.CustomOutputDir = dir
									s.SavePreferences()
								}
							})
						}
						if s.CustomOutputDir != "" && s.Bridge.OpenPath != nil {
							openDirBtn := ui.Button(c.Key("btn-open-outdir"), "").Label(cat.OpenDir).Tooltip(cat.OpenDir)
							openDirBtn.Children(func() {
								ui.Icon(c, IconExternal).FontSize(13)
							})
							if openDirBtn.Clicked() {
								s.Bridge.OpenPath(s.CustomOutputDir)
							}
						}
					}
				})
			}
		})
		ui.Box(c).FillWidth().Height(1).Background(pal.Divider)
	})
}

func (s *AppState) renderConvertPageCompact(c *ui.Context, cat i18n.Catalog, lang i18n.Lang, t *ui.Theme, pal SurfacePalette) {
	ui.Column(c.Key("page-convert-mobile")).Fill().Padding(10, 12).Gap(10).Clip().Children(func() {
		if len(s.Items) > 0 {
			ui.Row(c.Key("convert-toolbar-m")).FillWidth().Padding(8, 10).Radius(10).
				Background(pal.Card).Border(1, pal.CardBorder).
				Gap(8).Wrap().AlignItems(ui.Center).Children(func() {
				addBtn := ui.Button(c.Key("btn-add-files"), "")
				addBtn.Children(func() {
					ui.Icon(c, IconPlus).FontSize(14).TextColor(t.Accent)
					ui.Text(c, cat.AddFiles).FontWeight(600).SingleLine()
				})
				if addBtn.Clicked() && s.Bridge.PickFiles != nil {
					s.Bridge.PickFiles(s.Store.Get().LastOpenDir, func(paths []string) { s.AddQueueFiles(paths) })
				}
				ui.Spacer(c)
				if ui.Button(c.Key("btn-clear-all"), cat.ClearAll).Disabled(s.BatchRunning).Clicked() {
					s.ClearAll()
				}
				if s.BatchRunning {
					if ui.PrimaryButton(c.Key("btn-stop-all"), cat.CancelAll).Background(t.Danger).Clicked() {
						s.CancelAll()
					}
				} else {
					if ui.PrimaryButton(c.Key("btn-start-all"), cat.StartAll).Clicked() {
						s.StartAll()
					}
				}
			})

			ui.Row(c.Key("mobile-batch-bar")).FillWidth().Gap(8).Padding(7, 10).Radius(9).
				Background(pal.Subtle).Border(1, pal.SubtleBorder).AlignItems(ui.Center).Wrap().Children(func() {
				s.renderBatchTargetControls(c, cat, t)
			})
		}

		ui.Column(c.Key("queue-column")).Grow(1).FillWidth().Gap(9).Clip().Children(func() {
			s.renderDropZone(c, cat, lang, t, pal, len(s.Items) == 0, true)
			if len(s.Items) > 0 {
				ui.Scroll(c.Key("queue-scroll")).Grow(1).FillWidth().Gap(8).Children(func() {
					for _, item := range s.Items {
						it := item
						s.renderQueueCard(c, it, cat, lang, t, pal, true)
					}
				})
			}
		})
	})
}

func (s *AppState) renderBatchTargetControls(c *ui.Context, cat i18n.Catalog, t *ui.Theme) {
	batchTargets := s.AvailableBatchTargets()
	if len(batchTargets) == 0 {
		return
	}
	if s.BatchTarget == "" {
		s.BatchTarget = batchTargets[0]
	}
	ui.Row(c.Key("batch-target-bar")).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, cat.BatchTargetLabel+":").FontSize(11.5).TextColor(t.TextMuted).SingleLine()
		ui.Select(c.Key("sel-batch-target"), &s.BatchTarget, batchTargets).MinWidth(88).Width(88).Label(cat.BatchTargetLabel)
		if ui.Button(c.Key("btn-apply-batch"), cat.ApplyToAll).Clicked() {
			s.ApplyBatchTarget(s.BatchTarget)
		}
	})
}

func (s *AppState) renderDropZone(c *ui.Context, cat i18n.Catalog, lang i18n.Lang, t *ui.Theme, pal SurfacePalette, expanded bool, compact bool) {
	if !expanded {
		// Tactile Dashed Inline Append Strip above Active Queue
		strip := ui.Row(c.Key("drop-zone")).FillWidth().Padding(8, 14).Radius(10).
			Background(pal.Subtle).Border(1.5, pal.SubtleBorder).BorderStyle(ui.BorderDashed).
			Gap(10).AlignItems(ui.Center).Cursor(ui.CursorPointer)
		if files := strip.DroppedFiles(); len(files) > 0 {
			s.AddPaths(files)
		}
		if strip.Clicked() && s.Bridge.PickFiles != nil {
			s.Bridge.PickFiles(s.Store.Get().LastOpenDir, func(paths []string) {
				s.AddQueueFiles(paths)
			})
		}
		dragOver := strip.FileDragOver()
		if dragOver {
			strip.Border(1.5, t.Accent).BorderStyle(ui.BorderSolid).Background(t.Accent.Alpha(0.08))
		} else if strip.Hovered() {
			strip.Border(1.5, t.Accent.Alpha(0.45))
		}

		strip.Children(func() {
			ui.Box(c).Size(24, 24).Radius(6).Background(t.Accent.Alpha(0.12)).Center().Children(func() {
				ui.Icon(c, IconPlus).FontSize(13.5).TextColor(t.Accent)
			})
			hint := cat.DropAppendHint
			if dragOver {
				hint = cat.DropActive
			} else if compact {
				hint = cat.DropTitleMobile
			}
			ui.Text(c, hint).FontSize(12.5).FontWeight(600).TextColor(pal.TextSecondary).Grow(1).SingleLine().TruncateMiddle()

			_, _, doneCount, _, _ := s.QueueCounts()
			ui.Row(c).Padding(2, 9).Radius(99).Background(pal.Card).Border(1, pal.CardBorder).Children(func() {
				ui.Textf(c, "%d / %d", doneCount, len(s.Items)).
					FontSize(11).FontWeight(700).FontFeatures("tnum").TextColor(t.Accent).
					ContentTransition(ui.ContentTransition{Numeric: true}).SingleLine()
			})
		})
		return
	}

	// Full Empty-State Studio Dashed Drop Well (AudioSmith / lobe-mygo inspired)
	zone := ui.Column(c.Key("drop-zone")).FillWidth().Grow(1).Padding(28, 24).Radius(14).
		Background(pal.Subtle).Border(1.5, pal.SubtleBorder).BorderStyle(ui.BorderDashed).
		Center().Gap(14).Cursor(ui.CursorPointer)

	if files := zone.DroppedFiles(); len(files) > 0 {
		s.AddPaths(files)
	}
	if zone.Clicked() && s.Bridge.PickFiles != nil {
		s.Bridge.PickFiles(s.Store.Get().LastOpenDir, func(paths []string) {
			s.AddQueueFiles(paths)
		})
	}
	dragActive := zone.FileDragOver()
	if dragActive {
		zone.Border(2, t.Accent).BorderStyle(ui.BorderSolid).Background(t.Accent.Alpha(0.07)).
			AnimatePose(ui.Pose{Scale: 1.008}, ui.Snappy)
	} else if zone.Hovered() {
		zone.Border(1.5, t.Accent.Alpha(0.45))
	}

	zone.Children(func() {
		mascotW, mascotH := float32(188), float32(126)
		if compact {
			mascotW, mascotH = 168, 114
		}
		ui.Box(c).Padding(6, 14).Radius(99).
			RadialGradient(ui.RadialGradient{
				Stops: []ui.GradientStop{
					{Color: t.Accent.Alpha(0.12), At: 0},
					{Color: ui.Transparent, At: 1},
				},
			}).Center().Children(func() {
			ui.Image(c, MascotBitmap).Size(mascotW, mascotH)
		})

		ui.Column(c).Gap(5).AlignItems(ui.Center).Children(func() {
			if dragActive {
				ui.Text(c, cat.DropActive).FontSize(16.5).Bold().TextColor(t.Accent).TextAlign(ui.Center)
			} else if compact {
				ui.Text(c, cat.DropTitleMobile).FontSize(16).Bold().TextAlign(ui.Center)
				ui.Text(c, cat.DropHintMobile).FontSize(12.5).TextColor(t.TextMuted).TextAlign(ui.Center).MaxWidth(310)
			} else {
				ui.Text(c, cat.DropTitle).FontSize(16.5).Bold().TextAlign(ui.Center)
				ui.Text(c, cat.DropHint).FontSize(12.5).TextColor(t.TextMuted).TextAlign(ui.Center).MaxWidth(480)
			}
		})

		// Primary Action Cluster inside Drop Well
		ui.Row(c).Gap(8).Padding(2, 0).AlignItems(ui.Center).Children(func() {
			ctaBtn := ui.PrimaryButton(c.Key("btn-hero-pick"), "").Padding(8, 18)
			ctaBtn.Children(func() {
				ui.Icon(c, IconUpload).FontSize(14)
				ui.Text(c, cat.AddFiles).FontSize(13).FontWeight(600).SingleLine()
			})
			if ctaBtn.Clicked() && s.Bridge.PickFiles != nil {
				s.Bridge.PickFiles(s.Store.Get().LastOpenDir, func(paths []string) {
					s.AddQueueFiles(paths)
				})
			}

			if !compact && !s.Bridge.IsMobile && s.Bridge.PickFolder != nil {
				dirBtn := ui.Button(c.Key("btn-hero-folder"), "").Padding(8, 15).Background(pal.Card)
				dirBtn.Children(func() {
					ui.Icon(c, IconFolder).FontSize(14).TextColor(t.TextMuted)
					ui.Text(c, cat.AddFolder).FontSize(13).SingleLine()
				})
				if dirBtn.Clicked() {
					s.Bridge.PickFolder(s.Store.Get().LastOpenDir, func(dir string) {
						if dir != "" {
							s.AddPaths([]string{dir})
						}
					})
				}
			}
		})

		// Refined Format Capability Chips
		type pillSpec struct {
			cat converter.Category
			zh  string
			en  string
		}
		pills := []pillSpec{
			{converter.CategoryImage, "图片 PNG · WebP · SVG", "Image PNG · WebP"},
			{converter.CategoryPDF, "PDF 加解密 · 拆分", "PDF Split · AES"},
			{converter.CategoryDocument, "文档 DOCX · EPUB", "Docs DOCX · EPUB"},
			{converter.CategorySpreadsheet, "表格 XLSX · JSON", "Data XLSX · JSON"},
			{converter.CategoryArchive, "音视频与压缩 MP4 · ZIP", "Media & ZIP"},
		}
		ui.Row(c).Gap(6).Wrap().Justify(ui.Center).MaxWidth(540).Padding(4, 0, 0, 0).Children(func() {
			for _, pl := range pills {
				cp := CategoryPalette(pl.cat, t.Dark)
				lbl := pl.zh
				if lang == i18n.LangEN {
					lbl = pl.en
				}
				ui.Row(c).Padding(3, 9).Radius(6).Background(pal.Card).Border(1, pal.CardBorder).Gap(5).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, CategoryIcon(pl.cat)).FontSize(11.5).TextColor(cp.FG)
					ui.Text(c, lbl).FontSize(11).FontWeight(500).TextColor(pal.TextSecondary).SingleLine()
				})
			}
		})
	})
}

func (s *AppState) renderQueueCard(c *ui.Context, it *QueueItem, cat i18n.Catalog, lang i18n.Lang, t *ui.Theme, pal SurfacePalette, compact bool) {
	cp := CategoryPalette(it.Category, t.Dark)
	isBusy := it.Stage == converter.StageConverting || it.Stage == converter.StageQueued || it.Stage == converter.StagePreparing || it.Stage == converter.StageValidating

	card := ui.Column(c.Key(it.ID)).FillWidth().Padding(10, 12).Radius(10).
		Background(pal.Card).Border(1, pal.CardBorder).
		Shadow(0, 1, 3, 0, pal.CardShadow).Gap(8).
		Transition(ui.ElementTransition{Animation: &ui.Snappy})
	if t.Dark {
		card.InsetShadow(0, 1, 0, 0, pal.TopHighlight)
	}
	if card.Hovered() {
		card.Background(pal.CardHover).Border(1, pal.CardBorderHover)
	}

	// Drag converted output file directly out to Finder / Explorer / Desktop / Chat apps!
	if it.Stage == converter.StageDone && it.OutputPath != "" && !s.Bridge.IsMobile {
		outPath := it.OutputPath
		card.DragDataFrom(func() transfer.Data {
			d, _ := transfer.FileData(outPath)
			return d
		}, transfer.DragOptions{Operations: transfer.Copy})
	}

	// Native Right-Click Context Menu for power actions
	card.ContextMenu(func(m *ui.Menu) {
		if !isBusy {
			runLbl := cat.ActionConvert
			if it.Stage == converter.StageDone || it.Stage == converter.StageFailed || it.Stage == converter.StageCanceled {
				runLbl = cat.ActionRetry
			}
			if m.Item(runLbl).Disabled(s.BatchRunning).Chosen() {
				s.StartSingle(it.ID)
			}
		} else {
			if m.Item(cat.ActionCancel).Chosen() && s.ActiveBatch != nil {
				s.ActiveBatch.CancelJob(it.ID)
			}
		}
		if it.OutputPath != "" {
			m.Separator()
			if s.Bridge.OpenPath != nil {
				if m.Item(cat.ActionOpen).Chosen() {
					s.Bridge.OpenPath(it.OutputPath)
				}
			}
			if s.Bridge.RevealPath != nil && !s.Bridge.IsMobile {
				if m.Item(cat.ActionReveal).Chosen() {
					s.Bridge.RevealPath(it.OutputPath)
				}
			}
		}
		m.Separator()
		targetPath := it.Path
		if it.OutputPath != "" {
			targetPath = it.OutputPath
		}
		if m.Item(cat.ActionCopyPath).Chosen() {
			c.WriteClipboard(targetPath)
		}
		if !isBusy {
			if m.Item(cat.ActionRemove).Chosen() {
				s.RemoveItem(it.ID)
			}
		}
	})

	card.Children(func() {
		if compact {
			ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(36, 36).Radius(9).Background(cp.SoftBG).Border(1, cp.Border).Center().Shrink(0).Children(func() {
					ui.Icon(c, CategoryIcon(it.Category)).FontSize(17).TextColor(cp.FG)
				})
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).ClipX().Children(func() {
					ui.Text(c, it.Name).FontSize(13).FontWeight(600).SingleLine().TruncateMiddle()
					s.renderQueueMetaLine(c, it, lang, t, pal, true)
				})
				s.renderStageBadge(c, it, cat, t)
				if !isBusy {
					rmBtn := ui.Button(c.Key("rm-"+it.ID), "").Label(cat.ActionRemove).Padding(5, 7)
					rmBtn.Children(func() { ui.Icon(c, IconX).FontSize(13).TextColor(t.TextMuted) })
					if rmBtn.Clicked() {
						s.RemoveItem(it.ID)
					}
				}
			})

			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
				s.renderFormatCapsule(c, it, cp, t, pal, isBusy, 92)
				ui.Spacer(c)
				s.renderItemActionButtons(c, it, cat, t, false)
			})
		} else {
			// Spacious single-line desktop studio row with zero premature filename truncation
			ui.Row(c).FillWidth().Gap(9).AlignItems(ui.Center).Children(func() {
				// Consistent 3px vertical status accent bar so every row aligns on one crisp column
				barCol := pal.SubtleBorder
				if isBusy {
					barCol = t.Accent
				} else if it.Stage == converter.StageDone {
					barCol = t.Success
				} else if it.Stage == converter.StageFailed {
					barCol = t.Danger
				}
				ui.Box(c).Size(3, 30).Radius(2).Background(barCol).Shrink(0)

				ui.Box(c).Size(36, 36).Radius(9).Background(cp.SoftBG).Border(1, cp.Border).Center().Shrink(0).Children(func() {
					ui.Icon(c, CategoryIcon(it.Category)).FontSize(17).TextColor(cp.FG)
				})

				ui.Column(c).Grow(1).Shrink(1).MinWidth(120).Gap(2).ClipX().Children(func() {
					ui.Text(c, it.Name).FontSize(13).FontWeight(600).SingleLine().TruncateMiddle()
					s.renderQueueMetaLine(c, it, lang, t, pal, false)
				})

					s.renderFormatCapsule(c, it, cp, t, pal, isBusy, 88)

					stageColW := float32(68)
					actionColW := float32(116)
					if lang == i18n.LangEN {
						stageColW = 86
						actionColW = 124
					}
					ui.Row(c).MinWidth(stageColW).Justify(ui.Center).AlignItems(ui.Center).Shrink(0).Children(func() {
						s.renderStageBadge(c, it, cat, t)
					})
					ui.Row(c).MinWidth(actionColW).Justify(ui.End).AlignItems(ui.Center).Shrink(0).Children(func() {
						s.renderItemActionButtons(c, it, cat, t, true)
					})
			})
		}

		if it.Stage == converter.StageConverting || it.Stage == converter.StagePreparing || it.Stage == converter.StageValidating {
			ui.Progress(c.Key("prog-"+it.ID), it.Progress).Label(it.Name)
		}

		if it.ErrMsg != "" {
			ui.Row(c).FillWidth().Padding(6, 9).Radius(6).
				Background(t.Danger.Alpha(0.08)).Border(1, t.Danger.Alpha(0.20)).
				Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, IconAlert).FontSize(13).TextColor(t.Danger).Shrink(0)
				ui.Text(c, it.ErrMsg).FontSize(11.5).TextColor(t.Danger).Grow(1)
			})
		} else if len(it.Warnings) > 0 {
			ui.Text(c, strings.Join(it.Warnings, "; ")).FontSize(11.5).TextColor(t.Warning)
		}
	})
}

func (s *AppState) renderQueueMetaLine(c *ui.Context, it *QueueItem, lang i18n.Lang, t *ui.Theme, pal SurfacePalette, compact bool) {
	if it.Stage == converter.StageDone && it.OutputPath != "" {
		ui.Row(c).Gap(6).AlignItems(ui.Center).ClipX().Children(func() {
			ui.Textf(c, "%s → %s", HumanBytes(it.SizeBytes), HumanBytes(it.OutputSize)).
				FontSize(11.5).FontFeatures("tnum").TextColor(t.TextMuted).Shrink(0).SingleLine()

			if it.SizeBytes > 0 && it.OutputSize > 0 {
				diffPct := int(float64(it.OutputSize-it.SizeBytes) * 100.0 / float64(it.SizeBytes))
				if diffPct < 0 {
					ui.Row(c).Padding(1, 5).Radius(4).Background(t.Success.Alpha(0.13)).Shrink(0).Children(func() {
						ui.Textf(c, "%d%%", diffPct).FontSize(10.5).FontWeight(700).FontFeatures("tnum").TextColor(t.Success).SingleLine()
					})
				}
			}

			if it.Elapsed > 0 && !compact {
				ui.Textf(c, "· %v", it.Elapsed.Round(1e6)).
					FontSize(11).FontFeatures("tnum").TextColor(pal.TextFaint).Shrink(0).SingleLine()
			}
		})
		return
	}

	meta := fmt.Sprintf("%s  ·  %s", HumanBytes(it.SizeBytes), i18n.CategoryLabel(string(it.Category), lang))
	if it.Detail != "" {
		meta = fmt.Sprintf("%s  ·  %s", HumanBytes(it.SizeBytes), it.Detail)
	}
	ui.Text(c, meta).FontSize(11.5).FontFeatures("tnum").TextColor(t.TextMuted).SingleLine().TruncateMiddle()
}

func (s *AppState) renderFormatCapsule(c *ui.Context, it *QueueItem, cp CategoryColors, t *ui.Theme, pal SurfacePalette, isBusy bool, selW float32) {
	ui.Row(c).Padding(2, 3, 2, 8).Radius(7).
		Background(pal.Subtle).Border(1, pal.SubtleBorder).
		Gap(5).AlignItems(ui.Center).Shrink(0).Children(func() {
		ui.Row(c).MinWidth(30).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
			ui.Text(c, strings.ToUpper(it.SourceExt)).FontSize(10.5).FontWeight(700).TextColor(cp.FG).SingleLine()
		})
		ui.Icon(c, IconArrowRight).FontSize(11.5).TextColor(pal.TextFaint)
		s.renderItemTargetSelect(c, it, isBusy, selW)
	})
}

func (s *AppState) renderItemTargetSelect(c *ui.Context, it *QueueItem, isBusy bool, width float32) {
	if len(it.TargetOptions) == 0 {
		return
	}
	sel := ui.Select(c.Key("target-"+it.ID), &it.TargetExt, it.TargetOptions).MinWidth(width).Width(width).Disabled(isBusy).Label("Target")
	if sel.Changed() {
		s.Store.RememberTarget(it.SourceExt, it.TargetExt)
		if it.Stage == converter.StageDone || it.Stage == converter.StageFailed || it.Stage == converter.StageCanceled {
			it.Stage = converter.StagePending
			it.Progress = 0
			it.ErrMsg = ""
		}
	}
}

func (s *AppState) renderItemActionButtons(c *ui.Context, it *QueueItem, cat i18n.Catalog, t *ui.Theme, showRemove bool) {
	ui.Row(c).Gap(5).AlignItems(ui.Center).Shrink(0).Children(func() {
		switch it.Stage {
		case converter.StageDone:
			if it.OutputPath != "" && s.Bridge.SaveCopy != nil && s.Bridge.IsMobile {
				ui.PrimaryButton(c.Key("savecp-"+it.ID), cat.ActionSaveCopy).OnClick(func() {
					s.Bridge.SaveCopy(it.OutputPath, filepath.Base(it.OutputPath), func(saved string, err error) {
						if err == nil && saved != "" {
							s.PendingToast = cat.SavedToast
						}
					})
				})
			}
			if it.OutputPath != "" && s.Bridge.OpenPath != nil {
				ui.Button(c.Key("open-"+it.ID), cat.ActionOpen).Padding(5, 10).OnClick(func() {
					s.Bridge.OpenPath(it.OutputPath)
				})
			}
			if it.OutputPath != "" && s.Bridge.RevealPath != nil && !s.Bridge.IsMobile {
				revBtn := ui.Button(c.Key("rev-"+it.ID), "").Label(cat.ActionReveal).Tooltip(cat.ActionReveal).Padding(5, 8)
				revBtn.Children(func() {
					ui.Icon(c, IconFolderOpen).FontSize(13.5).TextColor(t.TextMuted)
				})
				if revBtn.Clicked() {
					s.Bridge.RevealPath(it.OutputPath)
				}
			}
			if showRemove {
				rmBtn := ui.Button(c.Key("rm-"+it.ID), "").Label(cat.ActionRemove).Tooltip(cat.ActionRemove).Padding(5, 7)
				rmBtn.Children(func() {
					ui.Icon(c, IconX).FontSize(13).TextColor(t.TextMuted)
				})
				if rmBtn.Clicked() {
					s.RemoveItem(it.ID)
				}
			}

		case converter.StageConverting, converter.StageQueued, converter.StagePreparing, converter.StageValidating:
			ui.Button(c.Key("cancel-"+it.ID), cat.ActionCancel).Padding(5, 10).OnClick(func() {
				if s.ActiveBatch != nil {
					s.ActiveBatch.CancelJob(it.ID)
				}
			})

		default:
			lbl := cat.ActionConvert
			if it.Stage == converter.StageFailed || it.Stage == converter.StageCanceled {
				lbl = cat.ActionRetry
			}
			ui.Button(c.Key("run-"+it.ID), lbl).Padding(5, 10).Disabled(s.BatchRunning).OnClick(func() {
				s.StartSingle(it.ID)
			})
			if showRemove {
				rmBtn := ui.Button(c.Key("rm-"+it.ID), "").Label(cat.ActionRemove).Tooltip(cat.ActionRemove).Padding(5, 7)
				rmBtn.Children(func() {
					ui.Icon(c, IconX).FontSize(13).TextColor(t.TextMuted)
				})
				if rmBtn.Clicked() {
					s.RemoveItem(it.ID)
				}
			}
		}
	})
}

func (s *AppState) renderStageBadge(c *ui.Context, it *QueueItem, cat i18n.Catalog, t *ui.Theme) {
	st := it.Stage
	pill := func(text string, bg, fg ui.Color, spinner bool) {
		ui.Row(c).Padding(3, 8).Radius(99).Background(bg).Gap(5).AlignItems(ui.Center).Shrink(0).Children(func() {
			if spinner {
				loading.Ring(c, loading.Options{Size: 11.5, Color: fg})
			}
			ui.Text(c, text).FontSize(11).FontWeight(600).FontFeatures("tnum").TextColor(fg).SingleLine()
		})
	}

	switch st {
	case converter.StageDone:
		pill(cat.StageDone, t.Success.Alpha(0.14), t.Success, false)
	case converter.StageFailed:
		pill(cat.StageFailed, t.Danger.Alpha(0.14), t.Danger, false)
	case converter.StageCanceled:
		pill(cat.StageCanceled, t.TextMuted.Alpha(0.14), t.TextMuted, false)
	case converter.StageConverting:
		lbl := cat.StageConverting
		if it.Progress > 0.01 && it.Progress < 0.99 {
			lbl = fmt.Sprintf("%d%%", int(it.Progress*100))
		}
		pill(lbl, t.Accent.Alpha(0.14), t.Accent, true)
	case converter.StageValidating:
		pill(cat.StageValidating, t.Accent.Alpha(0.14), t.Accent, true)
	case converter.StageQueued, converter.StagePreparing:
		pill(cat.StageQueued, t.Accent.Alpha(0.10), t.Accent, false)
	default:
		pill(cat.StagePending, t.TextMuted.Alpha(0.11), t.TextMuted, false)
	}
}

// --- Docked Conversion Parameters Inspector ---

func (s *AppState) renderOptionsInspector(c *ui.Context, cat i18n.Catalog, t *ui.Theme, pal SurfacePalette) {
	ui.Scroll(c.Key("inspector-panel")).Width(262).FillHeight().Padding(14, 15).
		Background(pal.HeaderBG).Gap(14).Shrink(0).Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(24, 24).Radius(6).Background(t.Accent.Alpha(0.12)).Center().Children(func() {
				ui.Icon(c, IconSliders).FontSize(13).TextColor(t.Accent)
			})
			ui.Text(c, cat.InspectorTitle).FontSize(13.5).Bold().Grow(1)
		})

		s.renderOptionsControls(c, cat, t, pal)

		if !s.Bridge.IsMobile && len(s.Items) == 0 {
			ui.Box(c).FillWidth().Height(1).Background(pal.Divider)
			ui.Column(c.Key("insp-outdir")).Gap(7).Children(func() {
				ui.Text(c, cat.OutputDirLabel).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
				if ui.Segmented(c.Key("seg-out-empty"), &s.OutputModeIdx, cat.SameAsSource, cat.CustomDir).FillEqually().Changed() {
					s.SavePreferences()
				}
				if s.OutputModeIdx == 1 {
					ui.Row(c).FillWidth().Gap(6).Children(func() {
						ui.TextInput(c.Key("inp-custom-dir-empty"), &s.CustomOutputDir).Grow(1).Placeholder("/path/to/output")
						if ui.Button(c.Key("btn-pick-outdir-empty"), "…").Tooltip(cat.ChooseDir).Clicked() && s.Bridge.PickFolder != nil {
							s.Bridge.PickFolder(s.CustomOutputDir, func(dir string) {
								if dir != "" {
									s.CustomOutputDir = dir
									s.SavePreferences()
								}
							})
						}
					})
				}
			})
		}
	})
}

func (s *AppState) renderOptionsControls(c *ui.Context, cat i18n.Catalog, t *ui.Theme, pal SurfacePalette) {
	sectionHeader := func(icon *ui.SVG, col ui.Color, label string) {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon).FontSize(12.5).TextColor(col)
			ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
		})
	}

	// Image Parameters Card
	imgPal := CategoryPalette(converter.CategoryImage, t.Dark)
	ui.Column(c.Key("opt-img")).Padding(11, 12).Radius(9).Background(pal.Subtle).Border(1, pal.SubtleBorder).Gap(8).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			sectionHeader(IconImage, imgPal.FG, cat.ImageQuality)
			ui.Spacer(c)
			ui.Row(c).Padding(1, 7).Radius(5).Background(t.Accent.Alpha(0.12)).Children(func() {
				ui.Textf(c, "%d%%", int(s.OptQuality)).FontSize(11.5).FontWeight(700).FontFeatures("tnum").TextColor(t.Accent).SingleLine()
			})
		})
		ui.StepSlider(c.Key("sld-quality"), &s.OptQuality, 10, 100, 1).Label(cat.ImageQuality)

		ui.Text(c, cat.ImageMaxWidth).FontSize(11.5).FontWeight(600).TextColor(pal.TextSecondary)
		ui.NumberInput(c.Key("num-maxw"), &s.OptMaxWidth, 0, 16384, 128).Label(cat.ImageMaxWidth)
		ui.Text(c, cat.ImageMaxWidthHint).FontSize(10.5).TextColor(t.TextMuted)
	})

	// Media Parameters (FFmpeg)
	if !s.Bridge.IsMobile {
		medPal := CategoryPalette(converter.CategoryVideo, t.Dark)
		ui.Column(c.Key("opt-media")).Padding(11, 12).Radius(9).Background(pal.Subtle).Border(1, pal.SubtleBorder).Gap(7).Children(func() {
			sectionHeader(IconFilm, medPal.FG, cat.VideoCodec)
			ui.Select(c.Key("sel-vcodec"), &s.OptVideoCodec, []string{"h264", "h265", "av1", "vp9", "copy"}).FillWidth().Label(cat.VideoCodec)

			ui.Text(c, cat.AudioBitrate).FontSize(11.5).FontWeight(600).TextColor(pal.TextSecondary)
			ui.Select(c.Key("sel-abitrate"), &s.OptAudioBitrate, []string{"128k", "192k", "256k", "320k"}).FillWidth().Label(cat.AudioBitrate)
		})
	}

	// PDF Parameters
	pdfPal := CategoryPalette(converter.CategoryPDF, t.Dark)
	ui.Column(c.Key("opt-pdf")).Padding(11, 12).Radius(9).Background(pal.Subtle).Border(1, pal.SubtleBorder).Gap(7).Children(func() {
		sectionHeader(IconFileText, pdfPal.FG, cat.PDFOperation)
		pdfModes := []string{
			cat.PDFOpConvert,
			cat.PDFOpSplit,
			cat.PDFOpEncrypt,
			cat.PDFOpDecrypt,
			cat.PDFOpExtractImages,
		}
		selectedLabel := pdfModes[0]
		if s.OptPDFModeIdx >= 0 && s.OptPDFModeIdx < len(pdfModes) {
			selectedLabel = pdfModes[s.OptPDFModeIdx]
		}
		if ui.Select(c.Key("sel-pdf-op"), &selectedLabel, pdfModes).FillWidth().Label(cat.PDFOperation).Changed() {
			for idx, lbl := range pdfModes {
				if lbl == selectedLabel {
					s.OptPDFModeIdx = idx
					break
				}
			}
		}
		if s.OptPDFModeIdx == 1 {
			ui.Text(c, cat.PDFSplitSpan).FontSize(11.5).TextColor(pal.TextSecondary)
			ui.NumberInput(c.Key("num-pdf-span"), &s.OptPDFSplitSpan, 1, 500, 1).Label(cat.PDFSplitSpan)
		}
		if s.OptPDFModeIdx == 2 || s.OptPDFModeIdx == 3 {
			ui.Text(c, cat.PDFPassword).FontSize(11.5).TextColor(pal.TextSecondary)
			ui.TextInput(c.Key("inp-pdf-pw"), &s.OptPDFPassword).Password().Placeholder("••••••••").Label(cat.PDFPassword)
			ui.Text(c, cat.PDFPasswordHint).FontSize(10.5).TextColor(t.TextMuted)
		}
	})

	// Source Text Encoding
	docPal := CategoryPalette(converter.CategoryDocument, t.Dark)
	ui.Column(c.Key("opt-enc")).Padding(11, 12).Radius(9).Background(pal.Subtle).Border(1, pal.SubtleBorder).Gap(7).Children(func() {
		sectionHeader(IconFileText, docPal.FG, cat.TextEncoding)
		ui.Select(c.Key("sel-encoding"), &s.OptTextEncoding, []string{"auto", "utf-8", "gb18030", "utf-16le", "utf-16be"}).FillWidth().Label(cat.TextEncoding)
	})
}

// --- Workspace 2: Merge Studio ---

func (s *AppState) renderMergePage(c *ui.Context, cat i18n.Catalog, lang i18n.Lang, t *ui.Theme, pal SurfacePalette, compact bool) {
	ext := "pdf"
	if s.MergeModeIdx == 2 {
		ext = "zip"
	}

	triggerMerge := func() {
		if !s.Bridge.IsMobile && s.Bridge.SaveFile != nil {
			stem := strings.TrimSpace(s.MergeOutputName)
			if stem == "" {
				stem = "merged-output"
			}
			defaultName := filepath.Join(filepath.Dir(s.MergeItems[0].Path), stem+"."+ext)
			s.Bridge.SaveFile(defaultName, ext, func(chosen string) {
				if chosen != "" {
					s.RunMergeTo(chosen)
				}
			})
		} else {
			s.RunMergeTo("")
		}
	}

	ui.Column(c.Key("page-merge")).Fill().Clip().Children(func() {
		if !compact {
			pill := ""
			if len(s.MergeItems) > 0 {
				var sum int64
				for _, m := range s.MergeItems {
					sum += m.SizeBytes
				}
				pill = fmt.Sprintf("%d · %s", len(s.MergeItems), HumanBytes(sum))
			}
			renderWorkspaceHeader(c, "merge-header", cat.NavMerge, pill, t, pal, func() {
				addBtn := ui.Button(c.Key("merge-add-files"), "").Tooltip(cat.AddFiles + " (⌘O)")
				addBtn.Children(func() {
					ui.Icon(c, IconPlus).FontSize(14).TextColor(t.Accent)
					ui.Text(c, cat.AddFiles).FontWeight(600).SingleLine()
				})
				if addBtn.Clicked() && s.Bridge.PickFiles != nil {
					s.Bridge.PickFiles(s.Store.Get().LastOpenDir, func(paths []string) { s.AddMergeFiles(paths) })
				}

				if len(s.MergeItems) > 0 {
					if ui.Button(c.Key("merge-clear"), cat.ClearAll).Disabled(s.MergeRunning).Clicked() {
						s.MergeItems = nil
						s.MergeLastOutput = ""
						s.MergeLastErr = ""
					}
				}

				ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
					ui.Text(c, cat.MergeOutputName+":").FontSize(12).TextColor(t.TextMuted).SingleLine()
					ui.TextInput(c.Key("inp-merge-name"), &s.MergeOutputName).Width(164).Placeholder("merged-output")
					ui.Row(c).Padding(4, 7).Radius(5).Background(pal.Subtle).Border(1, pal.SubtleBorder).Children(func() {
						ui.Text(c, "."+strings.ToUpper(ext)).FontSize(11).FontWeight(700).TextColor(t.Accent).SingleLine()
					})
				})

				runBtn := ui.PrimaryButton(c.Key("btn-run-merge"), cat.StartMerge).
					Disabled(len(s.MergeItems) == 0 || s.MergeRunning)
				if runBtn.Clicked() {
					triggerMerge()
				}
			})
		}

		padY, padX := float32(14), float32(16)
		if compact {
			padY, padX = 10, 12
		}
		ui.Column(c.Key("merge-body")).Grow(1).FillWidth().Padding(padY, padX).Gap(12).Clip().Children(func() {
			// Sleek Horizontal 3-Card Radio Mode Selector
			type modeCard struct {
				idx   int
				title string
				desc  string
				icon  *ui.SVG
				cat   converter.Category
			}
			modes := []modeCard{
				{0, cat.MergeImgTitle, cat.MergeImgDesc, IconImage, converter.CategoryImage},
				{1, cat.MergePDFTitle, cat.MergePDFDesc, IconFileText, converter.CategoryPDF},
				{2, cat.MergeZipTitle, cat.MergeZipDesc, IconArchive, converter.CategoryArchive},
			}
			if compact {
				modes[0].title = cat.MergeShortImgPDF
				modes[1].title = cat.MergeShortPDF
				modes[2].title = cat.MergeShortZip
			}

			ui.Row(c.Key("merge-mode-cards")).FillWidth().Gap(10).Children(func() {
				for _, mc := range modes {
					m := mc
					selected := s.MergeModeIdx == m.idx
					cp := CategoryPalette(m.cat, t.Dark)
					bg := pal.Card
					borderCol := pal.CardBorder
					if selected {
						borderCol = cp.FG
						if t.Dark {
							bg = cp.FG.Alpha(0.11)
						} else {
							bg = pal.Card
						}
					}

					card := ui.Row(c.Key(fmt.Sprintf("mcard-%d", m.idx))).Grow(1).Basis(0).
						Padding(11, 13).Radius(10).Background(bg).Border(1.5, borderCol).
						Shadow(0, 1, 3, 0, pal.CardShadow).Gap(11).AlignItems(ui.Center).
						Cursor(ui.CursorPointer)
					if !selected && card.Hovered() {
						card.Border(1.5, pal.CardBorderHover)
					}
					card.Children(func() {
						ui.Box(c).Size(36, 36).Radius(9).Background(cp.SoftBG).Border(1, cp.Border).Center().Shrink(0).Children(func() {
							ui.Icon(c, m.icon).FontSize(17).TextColor(cp.FG)
						})
						ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
							fg := t.Text
							if selected {
								fg = cp.FG
							}
							ui.Text(c, m.title).FontSize(13).FontWeight(700).TextColor(fg).SingleLine().TruncateMiddle()
							if !compact {
								ui.Text(c, m.desc).FontSize(11).TextColor(t.TextMuted).SingleLine().TruncateMiddle()
							}
						})
						if !compact {
							ringCol := pal.CardBorderHover
							if selected {
								ringCol = cp.FG
							}
							ui.Box(c).Size(16, 16).Radius(8).Border(1.5, ringCol).Center().Shrink(0).Children(func() {
								if selected {
									ui.Box(c).Size(8, 8).Radius(4).Background(cp.FG)
								}
							})
						}
					})
					if card.Clicked() && s.MergeModeIdx != m.idx {
						s.MergeModeIdx = m.idx
						s.MergeItems = nil
						s.MergeLastOutput = ""
						s.MergeLastErr = ""
					}
				}
			})

			if compact {
				ui.Row(c.Key("merge-toolbar-m")).FillWidth().Padding(8, 10).Radius(10).
					Background(pal.Card).Border(1, pal.CardBorder).Gap(8).Wrap().AlignItems(ui.Center).Children(func() {
					if ui.Button(c.Key("merge-add-m"), cat.AddFiles).Clicked() && s.Bridge.PickFiles != nil {
						s.Bridge.PickFiles(s.Store.Get().LastOpenDir, func(paths []string) { s.AddMergeFiles(paths) })
					}
					ui.TextInput(c.Key("inp-merge-name"), &s.MergeOutputName).Grow(1).Placeholder("merged-output")
					if ui.PrimaryButton(c.Key("btn-run-merge"), cat.StartMerge).Disabled(len(s.MergeItems) == 0 || s.MergeRunning).Clicked() {
						triggerMerge()
					}
				})
			}

			// Merge Drop Well & Ordered Item List
			mergeBox := ui.Column(c.Key("merge-drop")).Grow(1).FillWidth().Padding(14).Radius(13).
				Background(pal.Subtle).Border(1.5, pal.SubtleBorder).BorderStyle(ui.BorderDashed).
				Gap(8).Clip()
			if len(s.MergeItems) > 0 {
				mergeBox.BorderStyle(ui.BorderSolid).Background(pal.SubBarBG)
			}
			if dropped := mergeBox.DroppedFiles(); len(dropped) > 0 {
				s.AddMergeFiles(expandInputPaths(dropped))
			}
			if mergeBox.FileDragOver() {
				mergeBox.Border(2, t.Accent).BorderStyle(ui.BorderSolid).Background(t.Accent.Alpha(0.07))
			}

			mergeBox.Children(func() {
				if len(s.MergeItems) == 0 {
					empty := ui.Column(c.Key("merge-empty-tap")).Fill().Center().Gap(12).Cursor(ui.CursorPointer)
					if empty.Clicked() && s.Bridge.PickFiles != nil {
						s.Bridge.PickFiles(s.Store.Get().LastOpenDir, func(paths []string) { s.AddMergeFiles(paths) })
					}
					empty.Children(func() {
						ui.Image(c, MascotBitmap).Size(172, 116)
						ui.Text(c, cat.MergeEmptyHint).FontSize(13.5).FontWeight(500).TextColor(t.TextMuted).TextAlign(ui.Center).MaxWidth(520)
						pickBtn := ui.PrimaryButton(c.Key("merge-empty-btn"), "").Padding(8, 18)
						pickBtn.Children(func() {
							ui.Icon(c, IconUpload).FontSize(14)
							ui.Text(c, cat.AddFiles).FontWeight(600).SingleLine()
						})
						if pickBtn.Clicked() && s.Bridge.PickFiles != nil {
							s.Bridge.PickFiles(s.Store.Get().LastOpenDir, func(paths []string) { s.AddMergeFiles(paths) })
						}
					})
				} else {
					ui.Scroll(c.Key("merge-scroll")).Grow(1).FillWidth().Gap(7).Children(func() {
						for idx, mItem := range s.MergeItems {
							i := idx
							mi := mItem
							mCat := converter.CategoryForExt(mi.Ext)
							cp := CategoryPalette(mCat, t.Dark)
							row := ui.Row(c.Key(mi.ID)).FillWidth().Padding(9, 12).Radius(9).
								Background(pal.Card).Border(1, pal.CardBorder).
								Shadow(0, 1, 2, 0, pal.CardShadow).Gap(10).AlignItems(ui.Center).
								Transition(ui.ElementTransition{Animation: &ui.Snappy})
							row.ContextMenu(func(m *ui.Menu) {
								if m.Item(cat.MoveUp).Disabled(i == 0).Chosen() {
									s.MoveMergeItem(i, -1)
								}
								if m.Item(cat.MoveDown).Disabled(i == len(s.MergeItems)-1).Chosen() {
									s.MoveMergeItem(i, 1)
								}
								m.Separator()
								if m.Item(cat.ActionRemove).Chosen() {
									s.MergeItems = append(s.MergeItems[:i], s.MergeItems[i+1:]...)
								}
							})
							row.Children(func() {
								ui.Row(c).Padding(3, 7).Radius(5).Background(pal.Subtle).Border(1, pal.SubtleBorder).Children(func() {
									ui.Textf(c, "%02d", i+1).Font("monospace").FontSize(11).FontWeight(700).FontFeatures("tnum").TextColor(t.Accent).SingleLine()
								})
								ui.Box(c).Size(30, 30).Radius(7).Background(cp.SoftBG).Center().Shrink(0).Children(func() {
									ui.Icon(c, CategoryIcon(mCat)).FontSize(15).TextColor(cp.FG)
								})
								ui.Text(c, mi.Name).FontSize(13).FontWeight(600).Grow(1).SingleLine().TruncateMiddle()
								ui.Text(c, HumanBytes(mi.SizeBytes)).FontSize(11.5).FontFeatures("tnum").TextColor(t.TextMuted).SingleLine()

								upBtn := ui.Button(c.Key("up-"+mi.ID), "").Label(cat.MoveUp).Tooltip(cat.MoveUp).Padding(5, 7).Disabled(i == 0)
								upBtn.Children(func() { ui.Icon(c, IconArrowUp).FontSize(13) })
								if upBtn.Clicked() {
									s.MoveMergeItem(i, -1)
								}
								dnBtn := ui.Button(c.Key("dn-"+mi.ID), "").Label(cat.MoveDown).Tooltip(cat.MoveDown).Padding(5, 7).Disabled(i == len(s.MergeItems)-1)
								dnBtn.Children(func() { ui.Icon(c, IconArrowDown).FontSize(13) })
								if dnBtn.Clicked() {
									s.MoveMergeItem(i, 1)
								}
								rmBtn := ui.Button(c.Key("mrm-"+mi.ID), "").Label(cat.ActionRemove).Tooltip(cat.ActionRemove).Padding(5, 7)
								rmBtn.Children(func() { ui.Icon(c, IconX).FontSize(13).TextColor(t.TextMuted) })
								if rmBtn.Clicked() {
									s.MergeItems = append(s.MergeItems[:i], s.MergeItems[i+1:]...)
								}
							})
						}
					})
				}
			})

			if s.MergeLastErr != "" {
				ui.Row(c).FillWidth().Padding(8, 12).Radius(8).Background(t.Danger.Alpha(0.08)).Border(1, t.Danger.Alpha(0.22)).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, IconAlert).FontSize(14).TextColor(t.Danger)
					ui.Text(c, s.MergeLastErr).FontSize(12).TextColor(t.Danger).Grow(1)
				})
			} else if s.MergeLastOutput != "" {
				ui.Row(c).FillWidth().Padding(9, 12).Radius(9).Background(t.Success.Alpha(0.10)).Border(1, t.Success.Alpha(0.28)).
					Gap(10).AlignItems(ui.Center).Wrap().Children(func() {
					ui.Icon(c, IconCheck).FontSize(15).TextColor(t.Success)
					ui.Text(c, cat.MergeSuccessPrefix+filepath.Base(s.MergeLastOutput)).
						FontSize(12.5).FontWeight(600).TextColor(t.Success).Grow(1).SingleLine().TruncateMiddle()
					if s.Bridge.OpenPath != nil {
						ui.Button(c.Key("merge-open"), cat.ActionOpen).OnClick(func() { s.Bridge.OpenPath(s.MergeLastOutput) })
					}
					if s.Bridge.RevealPath != nil && !s.Bridge.IsMobile {
						ui.Button(c.Key("merge-reveal"), cat.ActionReveal).OnClick(func() { s.Bridge.RevealPath(s.MergeLastOutput) })
					}
				})
			}
		})
	})
	_ = lang
}

// --- Workspace 3: Engines & Active Format Matrix ---

func (s *AppState) renderEnginesPage(c *ui.Context, cat i18n.Catalog, lang i18n.Lang, t *ui.Theme, pal SurfacePalette, compact bool) {
	readyCount := 0
	for _, st := range s.EngineStatuses {
		if st.Available {
			readyCount++
		}
	}
	totalFormats := s.countUniqueFormats()

	ui.Column(c.Key("page-engines-root")).Fill().Clip().Children(func() {
		if !compact {
			pill := fmt.Sprintf("%d/%d %s · %d formats", readyCount, len(s.EngineStatuses), cat.EngineAvailable, totalFormats)
			if lang == i18n.LangZH {
				pill = fmt.Sprintf("%d/%d %s · %d 种格式", readyCount, len(s.EngineStatuses), cat.EngineAvailable, totalFormats)
			}
			renderWorkspaceHeader(c, "engines-header", cat.NavEngines, pill, t, pal, func() {
				if !s.Bridge.IsMobile {
					rescanBtn := ui.Button(c.Key("btn-rescan-engines"), "")
					rescanBtn.Children(func() {
						ui.Icon(c, IconRefresh).FontSize(13.5).TextColor(t.Accent)
						ui.Text(c, cat.RescanEngines).FontWeight(600).SingleLine()
					})
					if rescanBtn.Clicked() {
						s.Pipeline.Runtime().Refresh()
						s.RefreshEngineSnapshot()
					}
				}
			})
		}

		padY, padX := float32(16), float32(18)
		if compact {
			padY, padX = 12, 12
		}

		ui.Scroll(c.Key("page-engines")).Grow(1).FillWidth().Padding(padY, padX).Gap(14).Children(func() {
			// Featured Pure-Go Native Core Banner
			var externals []converter.EngineStatus
			for _, st := range s.EngineStatuses {
				if st.Builtin {
					ui.Row(c.Key("eng-builtin-hero")).FillWidth().Padding(14, 16).Radius(12).
						Background(pal.Card).Border(1, pal.CardBorder).Shadow(0, 1, 4, 0, pal.CardShadow).
						Gap(14).AlignItems(ui.Center).Wrap().Children(func() {
						ui.Box(c).Padding(4, 6).Radius(10).Background(t.Accent.Alpha(0.08)).Children(func() {
							ui.Image(c, MascotBitmap).Size(94, 64)
						})
						ui.Column(c).Grow(1).Shrink(1).MinWidth(200).Gap(5).Children(func() {
							ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
								ui.Text(c, cat.EngineBuiltinTitle).FontSize(14.5).Bold()
								ui.Row(c).Padding(2, 8).Radius(99).Background(t.Success.Alpha(0.14)).Gap(5).AlignItems(ui.Center).Children(func() {
									ui.Box(c).Size(6, 6).Radius(3).Background(t.Success)
									ui.Text(c, cat.EngineAvailable).FontSize(11).FontWeight(700).TextColor(t.Success).SingleLine()
								})
							})
							ui.Text(c, cat.EngineBuiltinDesc).FontSize(12).TextColor(t.TextMuted)
						})
					})
				} else if !s.Bridge.IsMobile || st.Available {
					externals = append(externals, st)
				}
			}

			// Responsive 2-Column CSS Grid for External Engines
			if len(externals) > 0 {
				cols := 2
				if compact {
					cols = 1
				}
				ui.Grid(c.Key("engines-grid")).Columns(cols).Gap(10).FillWidth().Children(func() {
					for _, st := range externals {
						eng := st
						card := ui.Column(c.Key("eng-" + string(eng.ID))).FillWidth().Padding(13, 14).Radius(10).
							Background(pal.Card).Border(1, pal.CardBorder).Shadow(0, 1, 3, 0, pal.CardShadow).Gap(8)
						if t.Dark {
							card.InsetShadow(0, 1, 0, 0, pal.TopHighlight)
						}
						card.Children(func() {
							ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
								iconBG := pal.Subtle
								iconFG := t.TextMuted
								if eng.Available {
									iconBG = t.Success.Alpha(0.13)
									iconFG = t.Success
								}
								ui.Box(c).Size(30, 30).Radius(8).Background(iconBG).Center().Shrink(0).Children(func() {
									ui.Icon(c, IconCPU).FontSize(15).TextColor(iconFG)
								})
								ui.Text(c, eng.Name).FontSize(13.5).Bold().Grow(1).SingleLine().TruncateMiddle()
								if eng.Available {
									ui.Row(c).Padding(2, 8).Radius(99).Background(t.Success.Alpha(0.14)).Gap(5).AlignItems(ui.Center).Children(func() {
										ui.Box(c).Size(5.5, 5.5).Radius(3).Background(t.Success)
										ui.Text(c, cat.EngineAvailable).FontSize(10.5).FontWeight(700).TextColor(t.Success).SingleLine()
									})
								} else {
									ui.Row(c).Padding(2, 8).Radius(99).Background(pal.Subtle).Border(1, pal.SubtleBorder).Children(func() {
										ui.Text(c, cat.EngineMissing).FontSize(10.5).FontWeight(600).TextColor(t.TextMuted).SingleLine()
									})
								}
							})

							ui.Text(c, localizedEngineDesc(eng, lang)).FontSize(11.5).TextColor(t.TextMuted).Grow(1)

							if eng.Path != "" {
								pathChip := ui.Row(c).FillWidth().Padding(5, 8).Radius(6).
									Background(pal.Subtle).Border(1, pal.SubtleBorder).Gap(6).AlignItems(ui.Center).
									Cursor(ui.CursorPointer).Tooltip(eng.Path)
								if pathChip.Clicked() {
									c.WriteClipboard(eng.Path)
								}
								pathChip.Children(func() {
									ui.Text(c, eng.Path).Font("monospace").FontSize(10.5).TextColor(pal.TextSecondary).Grow(1).SingleLine().TruncateMiddle()
								})
							} else if hint := engineInstallHint(eng.ID, lang); hint != "" {
								hintChip := ui.Row(c).FillWidth().Padding(5, 8).Radius(6).
									Background(pal.Subtle).Border(1, pal.SubtleBorder).Gap(6).AlignItems(ui.Center).
									Cursor(ui.CursorPointer).Tooltip(hint)
								if hintChip.Clicked() {
									c.WriteClipboard(hint)
								}
								hintChip.Children(func() {
									ui.Text(c, hint).Font("monospace").FontSize(10.5).TextColor(pal.TextFaint).Grow(1).SingleLine().TruncateMiddle()
								})
							}
						})
					}
				})
			}

			// Active Format Matrix Section Heading
			ui.Row(c).FillWidth().Padding(6, 2, 0, 2).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(3.5, 15).Radius(2).Background(t.Accent)
				ui.Text(c, cat.FormatsMatrixTitle).FontSize(14.5).Bold()
			})

			inLabel, outLabel := "输入", "输出"
			if lang == i18n.LangEN {
				inLabel, outLabel = "IN", "OUT"
			}
			matCols := 2
			if compact {
				matCols = 1
			}
			ui.Grid(c.Key("matrix-grid")).Columns(matCols).Gap(10).FillWidth().Children(func() {
				for _, g := range s.FormatMatrix {
					cp := CategoryPalette(g.Category, t.Dark)
					ui.Column(c.Key("mat-"+string(g.Category))).FillWidth().Padding(12, 14).Radius(10).
						Background(pal.Card).Border(1, pal.CardBorder).Shadow(0, 1, 3, 0, pal.CardShadow).
						Gap(8).Children(func() {
						ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Box(c).Size(28, 28).Radius(7).Background(cp.SoftBG).Border(1, cp.Border).Center().Children(func() {
								ui.Icon(c, CategoryIcon(g.Category)).FontSize(14).TextColor(cp.FG)
							})
							ui.Text(c, i18n.CategoryLabel(string(g.Category), lang)).FontSize(13.5).Bold().Grow(1)
							ui.Row(c).Padding(2, 8).Radius(99).Background(cp.SoftBG).Children(func() {
								ui.Textf(c, "%d → %d", len(g.Sources), len(g.Targets)).
									FontSize(10.5).FontWeight(700).FontFeatures("tnum").TextColor(cp.FG).SingleLine()
							})
						})

						ui.Row(c).FillWidth().Gap(4).Wrap().AlignItems(ui.Center).Children(func() {
							ui.Text(c, inLabel).Width(28).FontSize(10.5).FontWeight(600).TextColor(t.TextMuted).SingleLine()
							for _, src := range g.Sources {
								ui.Row(c).Padding(2, 6).Radius(5).Background(pal.Subtle).Border(1, pal.SubtleBorder).Children(func() {
									ui.Text(c, strings.ToUpper(src)).FontSize(10).FontWeight(600).TextColor(pal.TextSecondary).SingleLine()
								})
							}
						})

						ui.Row(c).FillWidth().Gap(4).Wrap().AlignItems(ui.Center).Children(func() {
							ui.Text(c, outLabel).Width(28).FontSize(10.5).FontWeight(700).TextColor(cp.FG).SingleLine()
							for _, dst := range g.Targets {
								ui.Row(c).Padding(2, 6).Radius(5).Background(cp.SoftBG).Border(1, cp.Border).Children(func() {
									ui.Text(c, strings.ToUpper(dst)).FontSize(10).Bold().TextColor(cp.FG).SingleLine()
								})
							}
						})
					})
				}
			})
		})
	})
}

// --- Workspace 4: Preferences (macOS System Settings Grouped Style) ---

func (s *AppState) renderSettingsPage(c *ui.Context, cat i18n.Catalog, lang i18n.Lang, t *ui.Theme, pal SurfacePalette, compact bool) {
	ui.Column(c.Key("page-settings-root")).Fill().Clip().Children(func() {
		if !compact {
			renderWorkspaceHeader(c, "settings-header", cat.SettingsTitle, "v0.1.0", t, pal, func() {
				if ui.Button(c.Key("btn-reset-mem-hdr"), cat.ResetFormatMemory).Clicked() {
					_ = s.Store.Update(func(p *config.Preferences) {
						clear(p.FormatMemory)
					})
					c.Toast(cat.FormatMemoryCleared)
				}
				if ui.PrimaryButton(c.Key("btn-save-prefs-hdr"), cat.SaveSettings).Clicked() {
					s.SavePreferences()
					c.Toast(cat.SettingsSavedToast)
				}
			})
		}

		padY, padX := float32(18), float32(22)
		if compact {
			padY, padX = 12, 12
		}

		ui.Scroll(c.Key("page-settings")).Grow(1).FillWidth().Padding(padY, padX).AlignItems(ui.Center).Gap(14).Children(func() {
			// 1. Compact Studio Brand Banner
			ui.Row(c.Key("settings-brand-card")).FillWidth().MaxWidth(760).Padding(13, 16).Radius(12).
				Background(pal.Card).Border(1, pal.CardBorder).Shadow(0, 1, 4, 0, pal.CardShadow).
				Gap(14).AlignItems(ui.Center).Children(func() {
				ui.Image(c, MascotBitmap).Size(78, 53)
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						renderBrandWordmark(c, t, 17.5)
						ui.Row(c).Padding(2, 7).Radius(99).Background(t.Accent.Alpha(0.12)).Children(func() {
							ui.Text(c, "v0.1.0").FontSize(10.5).FontWeight(700).FontFeatures("tnum").TextColor(t.Accent).SingleLine()
						})
					})
					ui.Text(c, cat.AppSubtitle).FontSize(12).TextColor(t.TextMuted)
				})
				if !compact {
					ui.Row(c).Padding(4, 9).Radius(6).Background(pal.Subtle).Border(1, pal.SubtleBorder).Children(func() {
						ui.Text(c, "Pure Go · GPU").Font("monospace").FontSize(11).FontWeight(600).TextColor(pal.TextSecondary).SingleLine()
					})
				}
			})

			// 2. Grouped General Preferences Card (Horizontal Label ↔ Compact Control Rows)
			ui.Column(c.Key("settings-general-group")).FillWidth().MaxWidth(760).Radius(12).
				Background(pal.Card).Border(1, pal.CardBorder).Shadow(0, 1, 4, 0, pal.CardShadow).Clip().Children(func() {

				renderSettingRow(c, cat.SettingLanguage, cat.DescLanguage, compact, t, func() {
					seg := ui.Segmented(c.Key("pref-lang"), &s.PrefLangIdx, cat.LangAutoLabel, cat.LangZHLabel, cat.LangENLabel).FillEqually()
					if !compact {
						seg.Width(272).Shrink(0)
					}
					if seg.Changed() {
						s.SavePreferences()
					}
				})

				ui.Box(c).FillWidth().Height(1).Background(pal.Divider)

				renderSettingRow(c, cat.SettingAppearance, cat.DescAppearance, compact, t, func() {
					seg := ui.Segmented(c.Key("pref-theme"), &s.PrefAppearanceIdx, cat.ThemeSystem, cat.ThemeLight, cat.ThemeDark).FillEqually()
					if !compact {
						seg.Width(272).Shrink(0)
					}
					if seg.Changed() {
						s.SavePreferences()
					}
				})

				if !s.Bridge.IsMobile {
					ui.Box(c).FillWidth().Height(1).Background(pal.Divider)
					renderSettingRow(c, cat.SettingOutputMode, cat.DescOutputMode, compact, t, func() {
						seg := ui.Segmented(c.Key("pref-outmode"), &s.OutputModeIdx, cat.SameAsSource, cat.CustomDir).FillEqually()
						if !compact {
							seg.Width(272).Shrink(0)
						}
						if seg.Changed() {
							s.SavePreferences()
						}
					})
					if s.OutputModeIdx == 1 {
						ui.Row(c).FillWidth().Padding(0, 16, 12, 16).Gap(8).AlignItems(ui.Center).Children(func() {
							ui.TextInput(c.Key("pref-custom-dir"), &s.CustomOutputDir).Font("monospace").Grow(1).Placeholder("/Users/.../Output")
							if ui.Button(c.Key("pref-pick-dir"), cat.ChooseDir).Clicked() && s.Bridge.PickFolder != nil {
								s.Bridge.PickFolder(s.CustomOutputDir, func(dir string) {
									if dir != "" {
										s.CustomOutputDir = dir
										s.SavePreferences()
									}
								})
							}
						})
					}
				}

				ui.Box(c).FillWidth().Height(1).Background(pal.Divider)

				renderSettingRow(c, cat.SettingConflict, cat.DescConflict, compact, t, func() {
					seg := ui.Segmented(c.Key("pref-conflict"), &s.PrefConflictIdx, cat.ConflictRename, cat.ConflictOverwrite).FillEqually()
					if !compact {
						seg.Width(272).Shrink(0)
					}
					if seg.Changed() {
						s.SavePreferences()
					}
				})

				ui.Box(c).FillWidth().Height(1).Background(pal.Divider)

				renderSettingRow(c, cat.SettingConcurrency, cat.DescConcurrency, compact, t, func() {
					row := ui.Row(c).Gap(10).AlignItems(ui.Center)
					if !compact {
						row.Width(272).Shrink(0)
					}
					row.Children(func() {
						sl := ui.StepSlider(c.Key("pref-workers"), &s.PrefConcurrency, 1, 8, 1).Label(cat.SettingConcurrency)
						if !compact {
							sl.Grow(1)
						} else {
							sl.Grow(1)
						}
						if sl.Changed() {
							s.SavePreferences()
						}
						ui.Row(c).Width(42).Padding(3, 8).Radius(6).Background(t.Accent.Alpha(0.12)).Center().Children(func() {
							ui.Textf(c, "%d", int(s.PrefConcurrency)).FontSize(12).FontWeight(700).FontFeatures("tnum").TextColor(t.Accent).SingleLine()
						})
					})
				})
			})

			// 3. Grouped External Binary Paths Card (Desktop)
			if !s.Bridge.IsMobile {
				ui.Column(c.Key("settings-paths-group")).FillWidth().MaxWidth(760).Padding(14, 16).Radius(12).
					Background(pal.Card).Border(1, pal.CardBorder).Shadow(0, 1, 4, 0, pal.CardShadow).
					Gap(10).Children(func() {
					ui.Text(c, cat.CustomEnginePaths).FontSize(13).FontWeight(700)

					s.renderEnginePathInput(c, converter.EngineFFmpeg, "FFmpeg", "path-ffmpeg", &s.PathFFmpeg, "/opt/homebrew/bin/ffmpeg", cat, t, pal)
					s.renderEnginePathInput(c, converter.EngineLibreOffice, "LibreOffice (soffice)", "path-lo", &s.PathLibreOffice, "/Applications/LibreOffice.app/Contents/MacOS/soffice", cat, t, pal)
					s.renderEnginePathInput(c, converter.EnginePandoc, "Pandoc", "path-pandoc", &s.PathPandoc, "/opt/homebrew/bin/pandoc", cat, t, pal)
					s.renderEnginePathInput(c, converter.EnginePoppler, "Poppler (pdftoppm)", "path-poppler", &s.PathPoppler, "/opt/homebrew/bin/pdftoppm", cat, t, pal)
					s.renderEnginePathInput(c, converter.EngineTesseract, "Tesseract OCR", "path-tess", &s.PathTesseract, "/opt/homebrew/bin/tesseract", cat, t, pal)
				})
			} else {
				ui.Row(c).FillWidth().Padding(4, 0).Gap(10).Wrap().Children(func() {
					if ui.Button(c.Key("btn-reset-mem"), cat.ResetFormatMemory).Clicked() {
						_ = s.Store.Update(func(p *config.Preferences) {
							clear(p.FormatMemory)
						})
						c.Toast(cat.FormatMemoryCleared)
					}
				})
			}
		})
	})
	_ = lang
}

func renderSettingRow(c *ui.Context, title, desc string, compact bool, t *ui.Theme, control func()) {
	if compact {
		ui.Column(c).FillWidth().Padding(12, 14).Gap(8).Children(func() {
			ui.Column(c).Gap(2).Children(func() {
				ui.Text(c, title).FontSize(13).FontWeight(600)
				ui.Text(c, desc).FontSize(11).TextColor(t.TextMuted)
			})
			control()
		})
		return
	}
	ui.Row(c).FillWidth().Padding(13, 16).Gap(16).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(13).FontWeight(600)
			ui.Text(c, desc).FontSize(11.5).TextColor(t.TextMuted)
		})
		control()
	})
}

func localizedEngineDesc(eng converter.EngineStatus, lang i18n.Lang) string {
	if lang != i18n.LangZH {
		return eng.Description
	}
	switch eng.ID {
	case converter.EngineFFmpeg:
		return "音视频专业编解码（MP3/FLAC/WAV/AAC/OPUS/MP4/MKV/MOV/WEBM/GIF）及 HEIC/AVIF/TGA 解码"
	case converter.EngineLibreOffice:
		return "高保真 Office 办公套件文档互转（DOC/DOCX/XLS/XLSX/PPT/PPTX/ODT/RTF ↔ PDF）"
	case converter.EnginePandoc:
		return "学术与富文本标记语言互转（Markdown / DOCX / EPUB / RST / LaTeX / HTML）"
	case converter.EnginePoppler:
		return "多页 PDF 高分辨率光栅化渲染导出（PNG / JPG 序列）"
	case converter.EngineTesseract:
		return "离线光学字符识别引擎（图片与扫描版 PDF → TXT / Markdown 文本提取）"
	case converter.EngineSips:
		return "macOS 原生 ImageIO 硬件加速图像与 PDF 首页渲染引擎"
	default:
		return eng.Description
	}
}

func engineInstallHint(id converter.EngineID, lang i18n.Lang) string {
	if runtime.GOOS == "darwin" {
		switch id {
		case converter.EngineFFmpeg:
			return "brew install ffmpeg"
		case converter.EngineLibreOffice:
			return "brew install --cask libreoffice"
		case converter.EnginePandoc:
			return "brew install pandoc"
		case converter.EnginePoppler:
			return "brew install poppler"
		case converter.EngineTesseract:
			return "brew install tesseract"
		}
	}
	if lang == i18n.LangZH {
		return "可在「偏好设置」中手动指定可执行文件路径"
	}
	return "Configure binary path in Preferences"
}

func (s *AppState) renderEnginePathInput(c *ui.Context, id converter.EngineID, label, key string, field *string, placeholder string, cat i18n.Catalog, t *ui.Theme, pal SurfacePalette) {
	avail := false
	for _, st := range s.EngineStatuses {
		if st.ID == id && st.Available {
			avail = true
			break
		}
	}
	dotCol := pal.TextFaint
	if avail {
		dotCol = t.Success
	}

	ui.Row(c.Key("row-"+key)).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Width(164).Gap(7).AlignItems(ui.Center).Shrink(0).Children(func() {
			ui.Box(c).Size(6.5, 6.5).Radius(4).Background(dotCol)
			ui.Text(c, label).FontSize(12.5).FontWeight(500).SingleLine()
		})
		ui.TextInput(c.Key(key), field).Font("monospace").Grow(1).Placeholder(placeholder).Label(label)
		if s.Bridge.PickFiles != nil {
			if ui.Button(c.Key("pick-"+key), cat.BrowseBinary).Padding(5, 10).Clicked() {
				s.Bridge.PickFiles("", func(paths []string) {
					if len(paths) > 0 {
						*field = paths[0]
						s.SavePreferences()
					}
				})
			}
		}
	})
}
