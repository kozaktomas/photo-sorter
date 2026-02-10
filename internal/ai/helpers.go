package ai

import (
	"fmt"
	"strings"
)

// buildAlbumDatePrompt returns the embedded album date estimation prompt.
func buildAlbumDatePrompt() string {
	return albumDatePrompt
}

// buildAlbumDateContent builds the user message content for album date estimation.
// This is shared across all AI providers.
func buildAlbumDateContent(albumTitle, albumDescription string, photoDescriptions []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Album title: %s\n", albumTitle)
	if albumDescription != "" {
		fmt.Fprintf(&b, "Album description: %s\n", albumDescription)
	}
	b.WriteString("\nPhoto descriptions:\n")
	for i, desc := range photoDescriptions {
		fmt.Fprintf(&b, "%d. %s\n", i+1, desc)
	}
	return b.String()
}
