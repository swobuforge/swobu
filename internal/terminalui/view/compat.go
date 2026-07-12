package view

import "github.com/swobuforge/swobu/internal/terminalui/transcript"

type RenderMode = transcript.RenderMode
type Retention = transcript.Retention
type ViewSpec = transcript.ViewSpec
type SceneSnapshot = transcript.SceneSnapshot
type PanelSpec = transcript.PanelSpec
type PanelBorderStyle = transcript.PanelBorderStyle
type FlowAxis = transcript.FlowAxis
type FlowSpec = transcript.FlowSpec
type GridSpec = transcript.GridSpec
type ScrollAxis = transcript.ScrollAxis
type ScrollSpec = transcript.ScrollSpec
type ShowSpec = transcript.ShowSpec
type TextSpec = transcript.TextSpec

const (
	RenderModeAppend     = transcript.RenderModeAppend
	RenderModeLive       = transcript.RenderModeLive
	RenderModeFullscreen = transcript.RenderModeFullscreen

	RetentionDurable   = transcript.RetentionDurable
	RetentionEphemeral = transcript.RetentionEphemeral

	FlowAxisColumn = transcript.FlowAxisColumn
	FlowAxisRow    = transcript.FlowAxisRow

	ScrollAxisY = transcript.ScrollAxisY
)

func DurableText(text string) ViewSpec { return transcript.DurableText(text) }

func EphemeralText(text string) ViewSpec { return transcript.EphemeralText(text) }

func Group(kind string, children ...ViewSpec) ViewSpec { return transcript.Group(kind, children...) }

func FlowColumn(kind string, gap int, children ...ViewSpec) ViewSpec {
	return transcript.FlowColumn(kind, gap, children...)
}

func FlowRow(kind string, gap int, children ...ViewSpec) ViewSpec {
	return transcript.FlowRow(kind, gap, children...)
}

func Normalize(root ViewSpec) ViewSpec { return transcript.Normalize(root) }

func DurableLines(root ViewSpec) []string { return transcript.DurableLines(root) }

func EphemeralLines(root ViewSpec) []string { return transcript.EphemeralLines(root) }

func Project(root ViewSpec) SceneSnapshot { return transcript.Project(root) }

func DurablePanel(spec PanelSpec) ViewSpec { return transcript.DurablePanel(spec) }
