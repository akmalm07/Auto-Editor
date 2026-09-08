package render

import (
	"auto-editor/internal/config"
	"auto-editor/internal/plan"
	"fmt"
	"os"
	"strings"
)

func WriteASS(path string, captions []plan.Caption, style config.CaptionConfig, width, height int) error {
	var out strings.Builder
	fmt.Fprintf(&out, "[Script Info]\nScriptType: v4.00+\nPlayResX: %d\nPlayResY: %d\nWrapStyle: 0\n\n", width, height)
	out.WriteString("[V4+ Styles]\nFormat: Name,Fontname,Fontsize,PrimaryColour,SecondaryColour,OutlineColour,BackColour,Bold,Italic,Underline,StrikeOut,ScaleX,ScaleY,Spacing,Angle,BorderStyle,Outline,Shadow,Alignment,MarginL,MarginR,MarginV,Encoding\n")
	fmt.Fprintf(&out, "Style: Default,%s,%d,&H00FFFFFF,&H000000FF,&H00000000,&H80000000,0,0,0,0,100,100,0,0,1,%d,0,2,80,80,%d,1\n\n", assField(style.FontFamily), style.FontSize, style.OutlineWidth, style.MarginBottom)
	out.WriteString("[Events]\nFormat: Layer,Start,End,Style,Name,MarginL,MarginR,MarginV,Effect,Text\n")
	for _, caption := range captions {
		fmt.Fprintf(&out, "Dialogue: 0,%s,%s,Default,,0,0,0,,%s\n", assTime(caption.StartMS), assTime(caption.EndMS), assText(caption.Text))
	}
	return os.WriteFile(path, []byte(out.String()), 0o644)
}

func assTime(ms int64) string {
	centiseconds := ms / 10
	hours := centiseconds / 360000
	minutes := (centiseconds / 6000) % 60
	seconds := (centiseconds / 100) % 60
	return fmt.Sprintf("%d:%02d:%02d.%02d", hours, minutes, seconds, centiseconds%100)
}
func assText(text string) string {
	text = strings.ReplaceAll(text, "\\", "\\\\")
	text = strings.ReplaceAll(text, "{", "\\{")
	text = strings.ReplaceAll(text, "}", "\\}")
	text = strings.ReplaceAll(text, "\r\n", "\\N")
	text = strings.ReplaceAll(text, "\n", "\\N")
	return text
}
func assField(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, ",", " "), "\n", " ")
}
