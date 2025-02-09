# jlv: JSON log viewer

## Overview

The JSON log viewer is a [bubbletea](https://github.com/charmbracelet/bubbletea)
TUI that helps explore logs made up of a series of JSON objects. It allows a
user to specify a `selector` which is the path to a field in the objects.
Objects without that field are hidden. The unique values of the field in the
objects will be used to populate the list.  When a value in the list is
selected, only ojects with that value for the selected field will be displayed.
The user can also specify an `output` format which will be used to select values
from each object to display.  By default, all objects are selected and pretty
printed. The equivalent `jq` command line is shown at the bottom of the screen.

<img width="1200" alt="A demo of the jlv application" src="screenshot.png">

## Install

```bash
go install github.com/mrxk/jlv@latest
```

## Requirements

* [jq](https://jqlang.org/)

## Usage

```
jlv

Usage:
        jlv [options] <path>

Options:
        -s <selector>, --selector=<selector> JSON path to grouping field.
        -o <format>, --output=<format>       Format of output.
```

## Key bindings

### Global

* `esc`: quit the application
* `tab`: change focus to the next TUI element
* `shift-tab`: change focus to the previous TUI element

### List window

* `/`: fliter the list
* `down`: select the next item
* `up`: select the previous item
* `right`: select the next page
* `left`: select the previous page
* `PageDown`: select the next page
* `PageUp`: select the previous page

### Output window

* `f`: toggle between full-screen and windowed view
* `w`: toggle between wrapped and truncated view
* `G`: scroll to the bottom
* `g`: scroll to the top
* `down`: scroll down
* `up`: scroll up
* `PageDown`: scroll down a page
* `PageUp`: scroll up a page

## Recorded demo

[demo](demo/README.md)
