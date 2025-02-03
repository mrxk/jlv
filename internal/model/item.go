package model

import "github.com/charmbracelet/bubbles/list"

var _ list.Item = (*item)(nil)

type item string

func (i item) FilterValue() string {
	return string(i)
}

func (i item) Title() string {
	return string(i)
}

func (i item) Description() string {
	return string(i)
}
