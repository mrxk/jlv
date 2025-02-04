package model

import "github.com/charmbracelet/bubbles/list"

// Ensure that item implements list.Item
var _ list.Item = (*item)(nil)

// item is a selectable list item based on a string.
type item string

// FilterValue is the value used when filtering against this item when filtering
// a list.
func (i item) FilterValue() string {
	return string(i)
}

// Title returns the title to display for this item in a list.
func (i item) Title() string {
	return string(i)
}

// Description returns the description to display for this item in a list.
func (i item) Description() string {
	return string(i)
}
