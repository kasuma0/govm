package styles

import (
	"fmt"
)

type Item struct {
	Name            string
	DescriptionText string
	Installed       bool
	Active          bool
}

func (i Item) Title() string {
	title := i.Name
	if i.Active {
		title = fmt.Sprintf("%s %s", title, SuccessStyle.Render("(active)"))
	}
	if i.Installed {
		title = fmt.Sprintf("%s %s", title, HighlightStyle.Render("(installed)"))
	}
	return title
}

func (i Item) FilterValue() string { return i.Name }
func (i Item) Description() string { return i.DescriptionText }
