package markdown

import (
	"fmt"
	"path/filepath"
)

// RelPath creates a relative path from baseDir to absPath
// absPath is the absolute path to the target file
// baseDir is the absolute path to the base directory
// Returns the relative path suitable for markdown links
func RelPath(absPath string, baseDir string) string {
	relPath, _ := filepath.Rel(baseDir, absPath)
	return relPath
}

// RelPathWithDisplay creates relative paths for both display and link
// absPath is the absolute path to the target file
// outputDir is the absolute path to the directory containing the output markdown file
// projectRoot is the absolute path to the project root for generating display text
// Returns: display text (relative to projectRoot), link path (relative to outputDir)
func RelPathWithDisplay(absPath string, outputDir string, projectRoot string) (string, string) {
	displayPath := RelPath(absPath, projectRoot)
	linkPath := RelPath(absPath, outputDir)
	return displayPath, linkPath
}

// Link creates a complete markdown link string [text](url)
// If text is empty, uses url as the display text
// url is the link URL
// text is the optional display text
func Link(url string, text ...string) string {
	displayText := url
	if len(text) > 0 && text[0] != "" {
		displayText = text[0]
	}
	return fmt.Sprintf("[%s](%s)", displayText, url)
}

// Code wraps text in markdown inline code backticks
func Code(text string) string {
	return fmt.Sprintf("`%s`", text)
}

// CodeLink creates a markdown link with code-formatted display text
// If text is empty, uses url as the display text
// url is the link URL
// text is the optional display text (will be wrapped in backticks)
func CodeLink(url string, text ...string) string {
	displayText := url
	if len(text) > 0 && text[0] != "" {
		displayText = text[0]
	}
	return Link(url, Code(displayText))
}
