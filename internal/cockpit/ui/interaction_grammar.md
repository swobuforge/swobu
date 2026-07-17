# Cockpit UI Interaction Grammar

**Status:** Canonical  
**Package:** `internal/cockpit/ui`

## Core Grammar

Cockpit UI gets exactly four product-facing verbs:

1. **Select** - Move the operator's current target
2. **Activate** - Run the selected target's primary action  
3. **Enter** - A selected target enters temporary local ownership
4. **Backout** - Escape exits the nearest entered owner

Everything else is rendering, product state, or low-level implementation detail.

---

## Select

Move the operator's current target.

**Inputs:**
- `Up` → previous selectable target
- `Down` → next selectable target

**Product concept:**
- `selection` (allowed public concept)

**Implementation:**
- Low-level focus implementation remains quarantined inside `ui` / `ui/interaction`
- Feature code should not talk about "focus"

---

## Activate

Run the selected target's primary action.

**Inputs:**
- `Enter` → activate
- `Space` → activate

**Product meanings:**
- Perform an action
- Enter a local mode/body
- Commit a local mode/body

**No extra interaction verbs for:**
- "open," "toggle," "choose," "submit," or "confirm"
- These are product meanings of activation

---

## Enter

A selected target may enter temporary local ownership.

**Examples:**
- `EditableRow` enters text editing
- `Select` enters body-visible state
- `ConfirmActionRow` enters confirmation state
- `SearchPicker`/`FileBrowser` live inside an entered `Select`

**This is the compression point.**

Avoid modeling these as separate public concepts:
- `expanded`, `opened`, `active`, `choosing`, `editing`, `confirming`, `armed`, `drilldown`, `subflow`, `wizard step`, `picker phase`

**They are all local entered states.**

---

## Backout

Escape exits the nearest entered owner.

**Input:**
- `Escape` → backout

**Rules:**
- `EditableRow` editing → exits edit mode
- `Select` entered → exits `Select` body
- `ConfirmActionRow` confirming → cancels confirmation
- `SearchPicker`/`FileBrowser` → calls parent backout
- No local owner → feature `BackScope` handles `Escape`

**The root feature must not know which child rows are entered.**

---

## Public Primitives

Feature packages may use only these interactive primitives:

```
ui.SelectableRow
ui.EditableRow
ui.Select
ui.SearchPicker
ui.FileBrowser
ui.ConfirmActionRow
ui.ActionTarget
ui.BackScope
```

That is the public grammar.

---

## Forbidden Vocabulary

### Feature code must not introduce:

- New focus handling in feature packages
- New picker phases
- New provider flows
- New per-row control structs
- New hidden form DSL

### Low-level APIs forbidden outside `ui` and `ui/interaction`:

```
tui.OnFocused
tui.OnPreemptStop
tui.WithFocusable
tui.WithOnFocus
tui.WithOnBlur
FocusNext
FocusPrev
focus trap
```

**Feature code consumes cockpit UI primitives. It does not author focus behavior.**

---

## Backout Hierarchy

```
Escape pressed
    ↓
Nearest entered owner handles it
    ↓
EditableRow editing → exits edit mode
    ↓
Select entered → exits Select body
    ↓
ConfirmActionRow confirming → cancels confirmation
    ↓
SearchPicker/FileBrowser → calls parent backout
    ↓
No local owner → feature BackScope handles Escape
    ↓
TargetConfig.Back() handles root concerns
```

---

## Component Behavior

### ui.Select

The canonical "selectable row + entered body" primitive.

**API:**
```go
type SelectProps struct {
    ID        string
    Label     string
    Value     string
    Action    string
    AutoFocus bool

    CanEnter  func() bool
    OnEnter   func()
    OnBackout func()

    Body func(backout func()) tui.Component
}
```

**Behavior:**
- `Enter` activates body
- Second activation backs out
- `Escape` on header backs out
- `CanEnter=false` refuses entry
- Body cancel calls backout
- `OnEnter` fires once per enter
- `OnBackout` fires once per backout

### ui.EditableRow

A row that enters text editing mode.

**Behavior:**
- `Enter` enters edit mode
- `Escape` exits edit mode
- `Enter` commits value

### ui.ConfirmActionRow

A row that enters confirmation state.

**Behavior:**
- `Enter` arms confirmation
- `Enter` again commits action
- `Escape` disarms

---

## Design Principles

### 1. Local Backout

Child components own their local backout. Root handles only root-level concerns.

**Bad:**
```go
func (w *TargetConfig) closeAnyOpenDisclosure() bool {
    if w.modelCtrl.close() { return true }
    if w.protocolCtrl.close() { return true }
    if w.credentialCtrl.close() { return true }
    return false
}
```

**Good:**
```go
func (w *TargetConfig) Back() bool {
    if w.DeleteArmed.Get() {
        w.DeleteArmed.Set(false)
        return true
    }

    if w.Phase.Get() == PhaseAuthPending {
        w.CancelAuthSession()
        return true
    }

    w.Close()
    return true
}
```

### 2. GSX Owns Structure

If a condition affects visual structure, put it in GSX.
If a condition affects behavior, put it in Go.

**Bad:**
```go
type TargetField struct {
    Visible func(*TargetConfig) bool
    Build   func(*TargetConfig) tui.Component
}
```

**Good:**
```go
templ TargetTail(w *TargetConfig) {
    @ModelSelect(w)

    if len(w.CurrentProtocolOptions()) > 0 {
        @ProtocolSelect(w)
    }

    if w.ShouldRenderPlacementRow() {
        @PlacementSelect(w)
    }

    @CreateOrSaveRow(w)
}
```

### 3. No Private UI Language

Feature packages use the cockpit grammar. They do not invent their own interaction verbs.

Feature code must not define:
- Provider flow components
- Control structs
- Picker phases
- Feature-level child backout lists
- Form field DSL

### 4. Delete Over Abstract

When refactoring, delete competing abstractions. Do not create replacement DSLs unless duplication becomes real and ugly.

Prefer:
- Explicit switch statements in GSX
- Helper predicates for readiness
- Direct field access

Over:
- Provider interface with `CanSave()`, `Render()`, etc.

---

## Acceptance

New cockpit UI features must:

- Use only the four public verbs (select, activate, enter, backout)
- Use only the public primitive components
- Keep child backout local
- Put visual structure in GSX
- Not introduce private interaction languages
- Not use raw go-tui interaction APIs

**The goal is boring code that reads like a form, not like a workflow engine.**
