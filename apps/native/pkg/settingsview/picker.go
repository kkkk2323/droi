package settingsview

import (
	"slices"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/brands"
	"github.com/kkkk2323/droi/apps/native/pkg/defaults"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/models"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
)

// PickerState is a model picker's own state while it is open.
type PickerState struct {
	filter    models.PickerFilter
	brand     models.Brand
	query     string
	highlight int
}

// brandIcon is a provider's mark, or sparkles for the Auto router.
func brandIcon(c *ui.Context, k *kit.Kit, b models.Brand, size float32, color ui.Color) ui.Element {
	if svg := brands.Mark(string(b)); svg != nil {
		return ui.Image(c, svg).Size(k.Px(size), k.Px(size)).Shrink(0).TextColor(color)
	}
	return k.Icon(c, "sparkles", size, color)
}

// ToolModeChoice is the picker's Tools row, offered only for a Session
// about to start: the Daemon fixes the mode when it creates the Session.
// An empty Value leaves no choice checked.
type ToolModeChoice struct {
	Value    defaults.ToolMode
	OnChange func(defaults.ToolMode)
}

// ModelPicker is the ModelPicker: a quiet trigger naming the model and
// its effort, opening a panel with the brands' rail, a search, the models,
// the reasoning effort and, when tools is set, the tool mode. The starred
// models are kept in store.
func ModelPicker(c *ui.Context, k *kit.Kit, store *prefs.Store, open *bool, st *PickerState, choices []models.Choice, value, effort string, onModel, onEffort func(string), tools *ToolModeChoice) {
	modelPickerAs(c, k, store, false, l10n.L("Model and reasoning effort"), "", false, open, st, choices, value, effort, onModel, onEffort, tools)
}

// ModelField is the ModelPicker's field look, for settings rows: a framed
// trigger opening the panel downwards, without the reasoning effort.
func ModelField(c *ui.Context, k *kit.Kit, store *prefs.Store, label string, disabled bool, open *bool, st *PickerState, choices []models.Choice, value string, onModel func(string)) {
	modelPickerAs(c, k, store, true, label, "", disabled, open, st, choices, value, "", onModel, nil, nil)
}

// ModelFieldOr is a ModelField whose list starts with fallback, a choice
// of no model of its own ("Inherit", "Same as main"): picking it calls
// onModel with "", and an empty value shows it.
func ModelFieldOr(c *ui.Context, k *kit.Kit, store *prefs.Store, label, fallback string, disabled bool, open *bool, st *PickerState, choices []models.Choice, value string, onModel func(string)) {
	modelPickerAs(c, k, store, true, label, fallback, disabled, open, st, choices, value, "", onModel, nil, nil)
}

func modelPickerAs(c *ui.Context, k *kit.Kit, store *prefs.Store, field bool, name, fallback string, disabled bool, open *bool, st *PickerState, choices []models.Choice, value, effort string, onModel, onEffort func(string), tools *ToolModeChoice) {
	t := k.T
	var current *models.Choice
	for i := range choices {
		if choices[i].ID == value {
			current = &choices[i]
		}
	}
	var levels []string
	if current != nil {
		levels = current.ReasoningEfforts
	} else if effort != "" {
		levels = []string{effort}
	}
	if effort != "" && !slices.Contains(levels, effort) {
		levels = append([]string{effort}, levels...)
	}
	var tr ui.Element
	fg := t.Foreground.Alpha(0.75)
	size, lh := float32(13), float32(19.5)
	if field {
		tr = ui.ButtonBase(c).Label(name).Expanded(*open).Height(k.Px(32)).MinWidth(k.Px(128)).Gap(k.Px(6)).PaddingX(k.Px(10)).
			Radius(k.Px(10)).Border(1, t.Border).Background(t.Background).Cursor(ui.CursorPointer)
		if tr.Hovered() || *open {
			tr.Background(t.Muted.Alpha(0.6))
		}
		fg, size, lh = t.Foreground, 14, 20
	} else {
		tr = k.QuietTrigger(c, name, *open).MaxWidth(k.Px(256))
		if tr.Hovered() || *open {
			fg = t.Foreground
		}
	}
	tr.Disabled(disabled || len(choices) == 0)
	tr.Children(func() {
		label := value
		if current != nil {
			brandIcon(c, k, models.BrandOf(current.ID, current.Provider), 14, fg)
			label = current.Label
		}
		switch {
		case label != "":
		case fallback != "":
			label = fallback
		case field:
			label = name
		default:
			label = l10n.L("Model")
		}
		l := k.Text(c, label, size, lh).TextColor(fg).SingleLine().Shrink(1)
		if field {
			l.Grow(1)
		}
		if effort != "" && len(levels) > 0 {
			k.Text(c, EffortLabel(effort), size, lh).TextColor(t.MutedForeground).Shrink(0)
		}
		if tools != nil && tools.Value != "" && tools.Value != defaults.DirectOnly {
			k.Text(c, l10n.T(defaults.ToolModeLabels[tools.Value]), size, lh).TextColor(t.MutedForeground).Shrink(0)
		}
		k.Icon(c, "chevron-down", 12, fg).Opacity(0.6)
	})
	if tr.Clicked() {
		*open = !*open
		if *open {
			// Open where the current model lives: Magpie's models are not in
			// the list the picker opens on, so a Session on one would look
			// like it had none. Otherwise the starred models, when the Daemon
			// offers any of them, are the ones picked most.
			*st = PickerState{filter: models.FilterAll}
			switch {
			case models.IsMagpie(value):
				st.filter = models.FilterMagpie
			case len(models.VisibleModels(choices, prefs.FavoriteModels.Get(store), models.FilterFavorites, "")) > 0:
				st.filter = models.FilterFavorites
			}
		}
	}
	if !*open {
		return
	}
	favorites := prefs.FavoriteModels.Get(store)
	filter := st.filter
	if st.brand != "" {
		filter = models.PickerFilter(st.brand)
	}
	rows := models.VisibleModels(choices, favorites, filter, st.query)
	searching := st.query != ""
	if fallback != "" && !searching && (filter == models.FilterAll || filter == models.FilterFavorites) {
		rows = append([]models.Row{{Choice: models.Choice{Label: fallback}}}, rows...)
	}
	active := min(st.highlight, max(len(rows)-1, 0))
	pick := func(id string) {
		if id != value {
			onModel(id)
		}
		*open = false
	}
	ui.PopoverBase(c, tr, open, func(p ui.Element) {
		if field {
			p.AttachTo(tr, ui.AnchorBottomRight, ui.AnchorTopRight).Margin(k.Px(4), 0, 0, 0)
		} else {
			p.AttachTo(tr, ui.AnchorTopLeft, ui.AnchorBottomLeft).Margin(0, 0, k.Px(6), 0)
		}
		p.Label(l10n.L("Choose a model")).
			Size(k.Px(480), k.Px(352)).Row().AlignItems(ui.Stretch).Radius(k.Px(12)).Border(1, t.Border).Background(t.Popover).
			TextColor(t.PopoverForeground).Clip().Shadow(0, k.Px(20), k.Px(25), -k.Px(5), ui.RGBA(0, 0, 0, 0.1))
		// The rail scrolls: the Daemon can offer more brands than its height holds.
		ui.Scroll(c).Width(k.Px(56)).Shrink(0).MinHeight(0).BorderWidth(0, 1, 0, 0).BorderColor(t.Border).Background(t.Sidebar).Children(func() {
			ui.Column(c).Role(ui.RoleToolbar).Label(l10n.L("Filter models")).FillWidth().Gap(k.Px(8)).Padding(k.Px(12), k.Px(8)).AlignItems(ui.Center).Children(func() {
				rail := func(label string, pressed bool, icon func(color ui.Color)) bool {
					b := ui.ButtonBase(c).Label(label).Tooltip(label).Checked(pressed).Size(k.Px(36), k.Px(36)).Radius(k.Px(8)).Shrink(0).Cursor(ui.CursorPointer)
					color := t.MutedForeground
					switch {
					case pressed:
						b.Background(t.SidebarAccent)
						color = t.Foreground
					case b.Hovered():
						b.Background(t.SidebarAccent.Alpha(0.6))
						color = t.Foreground
					}
					b.Children(func() { icon(color) })
					return b.Clicked()
				}
				fav := st.filter == models.FilterFavorites && st.brand == ""
				if rail(l10n.L("Favorites"), fav && !searching, func(color ui.Color) { k.Icon(c, "star", 14, color) }) {
					st.brand = ""
					st.filter = models.FilterFavorites
					if fav {
						st.filter = models.FilterAll
					}
					st.highlight = 0
				}
				ui.Box(c).Size(k.Px(20), 1).Margin(k.Px(4), 0).Background(t.Border).Shrink(0)
				// Magpie's models have a tab of their own: the gateway serves
				// models named like the Daemon's own, and beside them the list
				// reads as one crowd.
				if models.HasMagpie(choices) {
					pressed := st.filter == models.FilterMagpie && st.brand == ""
					if rail(l10n.L("Magpie"), pressed && !searching, func(color ui.Color) { k.Icon(c, "plug", 14, color) }) {
						st.brand = ""
						st.filter = models.FilterMagpie
						if pressed {
							st.filter = models.FilterAll
						}
						st.highlight = 0
					}
					ui.Box(c).Size(k.Px(20), 1).Margin(k.Px(4), 0).Background(t.Border).Shrink(0)
				}
				for _, b := range models.BrandsOf(choices) {
					pressed := st.brand == b
					if rail(l10n.T(models.BrandLabels[b]), pressed && !searching, func(color ui.Color) { brandIcon(c, k, b, 14, color) }) {
						st.filter = models.FilterAll
						st.brand = b
						if pressed {
							st.brand = ""
						}
						st.highlight = 0
					}
				}
			})
		})
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Row(c).Gap(k.Px(8)).PaddingX(k.Px(12)).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
				k.Icon(c, "search", 14, t.MutedForeground)
				in := ui.TextInputBase(c, &st.query).Label(l10n.L("Search models")).Placeholder(l10n.L("Search models…")).AutoFocus().
					Height(k.Px(40)).Grow(1).MinWidth(0).FontSize(k.Px(14)).TextColor(t.Foreground)
				if in.Changed() {
					st.highlight = 0
				}
				switch {
				case in.Shortcut(0, ui.KeyDown):
					st.highlight = min(active+1, len(rows)-1)
				case in.Shortcut(0, ui.KeyUp):
					st.highlight = max(active-1, 0)
				case in.Submitted():
					if active < len(rows) && !rows[active].Disabled {
						pick(rows[active].ID)
					}
				}
			})
			ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
				ui.Column(c).Role(ui.RoleList).Label(l10n.L("Models")).Padding(k.Px(6)).Children(func() {
					if len(rows) == 0 {
						msg := l10n.L("No models.")
						switch {
						case searching:
							msg = l10n.L("No models match.")
						case st.filter == models.FilterFavorites && st.brand == "":
							msg = l10n.L("Star a model to keep it here.")
						}
						ui.Box(c).Height(k.Px(240)).Center().Children(func() {
							k.Text(c, msg, 12, 16).TextColor(t.MutedForeground)
						})
					}
					for i, row := range rows {
						if row.ID == "" {
							fallbackRow(c, k, row.Label, i == active, value == "", func() { st.highlight = i }, func() { pick("") })
							continue
						}
						modelRow(c, k, store, row, i == active, row.ID == value, slices.Contains(favorites, row.ID), func() { st.highlight = i }, func() {
							if !row.Disabled {
								pick(row.ID)
							}
						})
					}
				})
			})
			showEffort := len(levels) > 0 && onEffort != nil
			if showEffort || tools != nil {
				ui.Column(c).Gap(k.Px(6)).Padding(k.Px(8), k.Px(12)).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
					segmented := func(title, name, value string, opts []kit.Option, onChange func(string)) {
						ui.Row(c).Gap(k.Px(12)).AlignItems(ui.Center).Children(func() {
							k.Text(c, title, 12, 16).TextColor(t.MutedForeground).Width(k.Px(64)).Shrink(0)
							if next, ok := k.Segmented(c, name, value, opts); ok {
								onChange(next)
							}
						})
					}
					if showEffort {
						opts := make([]kit.Option, len(levels))
						for i, l := range levels {
							opts[i] = kit.Option{Value: l, Label: EffortLabel(l)}
						}
						segmented(l10n.L("Reasoning"), l10n.L("Reasoning effort"), effort, opts, onEffort)
					}
					if tools != nil {
						opts := make([]kit.Option, len(defaults.ToolModes))
						for i, m := range defaults.ToolModes {
							opts[i] = kit.Option{Value: string(m), Label: l10n.T(defaults.ToolModeLabels[m])}
						}
						segmented(l10n.L("Tools"), l10n.L("Tool calls"), string(tools.Value), opts, func(v string) { tools.OnChange(defaults.ToolMode(v)) })
					}
				})
			}
		})
	})
}

// fallbackRow is the list's choice of no model of its own.
func fallbackRow(c *ui.Context, k *kit.Kit, label string, highlighted, selected bool, hover, click func()) {
	t := k.T
	r := kit.Selected(ui.Row(c.Key("fallback")).Role(ui.RoleListItem).Label(label), selected).Padding(k.Px(8), k.Px(10)).
		Radius(k.Px(8)).Cursor(ui.CursorPointer)
	if _, _, over := r.PointerPosition(); over && !highlighted {
		hover()
	}
	fg := t.PopoverForeground
	switch {
	case highlighted:
		r.Background(t.Accent)
		fg = t.AccentForeground
	case selected:
		r.Background(t.Muted)
	}
	r.Children(func() {
		k.Text(c, label, 13, 19.5).FontWeight(500).TextColor(fg).SingleLine()
	})
	if r.Clicked() {
		click()
	}
}

func modelRow(c *ui.Context, k *kit.Kit, store *prefs.Store, row models.Row, highlighted, selected, starred bool, hover, click func()) {
	t := k.T
	r := kit.Selected(ui.Row(c.Key(row.ID)).Role(ui.RoleListItem).Label(row.Label), selected).Gap(k.Px(12)).Padding(k.Px(8), k.Px(10)).
		Radius(k.Px(8)).Cursor(ui.CursorPointer)
	if _, _, over := r.PointerPosition(); over && !highlighted {
		hover()
	}
	fg := t.PopoverForeground
	switch {
	case highlighted:
		r.Background(t.Accent)
		fg = t.AccentForeground
	case selected:
		r.Background(t.Muted)
	}
	if row.Disabled {
		r.Opacity(0.5)
	}
	r.Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			k.Text(c, row.Label, 13, 19.5).FontWeight(500).TextColor(fg).SingleLine()
			ui.Row(c).Gap(k.Px(6)).Children(func() {
				brandIcon(c, k, row.Brand, 12, t.MutedForeground)
				source := l10n.T(models.BrandLabels[row.Brand])
				if row.Provider == "" {
					source = l10n.L("Router")
				}
				k.Text(c, source, 11, 16.5).TextColor(t.MutedForeground).SingleLine()
			})
		})
		if row.Multiplier != nil {
			k.Text(c, models.FormatMultiplier(*row.Multiplier), 11, 16.5).Font(k.Mono).TextColor(t.MutedForeground).Tooltip(l10n.L("Usage multiplier")).
				FontFeatures("tnum").Shrink(0)
		}
		label := l10n.L("Star %s", row.Label)
		if starred {
			label = l10n.L("Unstar %s", row.Label)
		}
		star := ui.ButtonBase(c).Label(label).Checked(starred).Size(k.Px(28), k.Px(28)).Radius(k.Px(6)).Shrink(0).Cursor(ui.CursorPointer)
		color := t.MutedForeground
		if starred {
			color = t.Attention
		} else if !highlighted && !star.Focused() {
			star.Opacity(0)
		}
		if star.Hovered() {
			star.Background(t.Background.Alpha(0.6))
		}
		star.Children(func() {
			ic := k.Icon(c, "star", 16, color)
			if starred {
				ic.Label(l10n.L("Starred"))
			}
		})
		if star.Clicked() {
			prefs.FavoriteModels.Toggle(store, row.ID)
			return
		}
	})
	if r.Clicked() {
		click()
	}
}

// EffortLabel is how a reasoning effort reads, in English in every language.
func EffortLabel(e string) string {
	if l, ok := models.EffortLabels[e]; ok {
		return l
	}
	return e
}
