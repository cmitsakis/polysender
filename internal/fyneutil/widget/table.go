package widget

import (
	"fmt"
	"reflect"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"go.polysender.org/internal/dbutil"
)

type TableAttribute struct {
	Name    string
	Field   string
	Actions bool
	Width   float32
}

type Table struct {
	widget.Table
	values      []dbutil.Saveable
	attributes  []TableAttribute
	actions     []Action
	refreshChan chan<- struct{}
}

func NewTable(refreshChan chan<- struct{}, attrs []TableAttribute, actions ...Action) *Table {
	t := &Table{
		attributes: attrs,
		actions:    actions,
	}
	t.refreshChan = refreshChan
	t.Length = func() (int, int) {
		return 0, len(t.attributes)
	}
	t.CreateCell = func() fyne.CanvasObject {
		c := container.NewHBox()
		c.Add(widget.NewLabel(""))
		for _, a := range actions {
			c.Add(widget.NewButtonWithIcon(a.Name, a.Icon, nil))
		}
		return c
	}
	t.Table.UpdateCell = func(id widget.TableCellID, cellCanvas fyne.CanvasObject) {}
	t.ExtendBaseWidget(t)
	for i, attr := range attrs {
		if attr.Width > 0 {
			t.SetColumnWidth(i, attr.Width)
		}
	}
	return t
}

func (t *Table) UpdateAndRefresh(values []dbutil.Saveable) {
	t.values = values
	t.Length = func() (int, int) {
		return len(t.values) + 1, len(t.attributes)
	}
	t.UpdateCell = func(id widget.TableCellID, cellCanvas fyne.CanvasObject) {
		cellAttribute := t.attributes[id.Col]
		if id.Row == 0 {
			cellCanvas.(*fyne.Container).Objects[0].(*widget.Label).SetText(cellAttribute.Name)
			for i := range t.actions {
				cellCanvas.(*fyne.Container).Objects[i+1].(*widget.Button).Hide()
			}
			return
		}
		index := id.Row - 1
		// check if index is out of range
		if index > len(t.values) {
			return
		}
		value := t.values[index]
		if cellAttribute.Actions {
			// clear text because sometimes it is not empty. Why?
			cellCanvas.(*fyne.Container).Objects[0].(*widget.Label).SetText("")
			// enable buttons
			for i, a := range t.actions {
				cellCanvas.(*fyne.Container).Objects[i+1].(*widget.Button).OnTapped = a.Func(value, t.refreshChan)
				// show button because sometimes it is hidden. Why?
				cellCanvas.(*fyne.Container).Objects[i+1].(*widget.Button).Show()
			}
		} else {
			cellValue, err := getFieldOrMethod(value, cellAttribute.Field)
			if err != nil {
				panic(err)
			}
			// set label's text
			maxChars := 23
			if cellAttribute.Width > 0 {
				maxChars = int(cellAttribute.Width/7) - 5
			}
			cellCanvas.(*fyne.Container).Objects[0].(*widget.Label).SetText(removeNewlines(firstNRunes(cellValue, maxChars)))
			// hide buttons
			for i := range t.actions {
				cellCanvas.(*fyne.Container).Objects[i+1].(*widget.Button).Hide()
			}
		}
	}
	t.Refresh()
}

func firstNRunes(str string, n int) string {
	i := 0
	for j := range str {
		if i >= n {
			if len(str[j:]) > 2 {
				var buf strings.Builder
				buf.WriteString(str[:j])
				buf.WriteString("...")
				return buf.String()
			} else if len(str[j:]) > 0 {
				return str
			} else {
				return str[:j]
			}
		}
		i++
	}
	return str
}

func removeNewlines(s string) string {
	m := func(r rune) rune {
		if r == '\n' || r == '\r' {
			return ' '
		}
		return r
	}
	return strings.Map(m, s)
}

func getFieldOrMethod(obj interface{}, name string) (string, error) {
	s, err := getField(obj, name)
	if err != nil {
		s, err = callMethod(obj, name)
		if err != nil {
			return "", fmt.Errorf("failed accessing field and method named %s: %s", name, err)
		}
	}
	return s, nil
}

func getField(obj interface{}, name string) (string, error) {
	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	ret := v.FieldByName(name)
	if !ret.IsValid() {
		return "", fmt.Errorf("ret invalid")
	}
	return fmt.Sprintf("%v", ret.Interface()), nil
}

func callMethod(obj interface{}, name string) (string, error) {
	v := reflect.ValueOf(obj)
	method := v.MethodByName(name)
	if !method.IsValid() {
		return "", fmt.Errorf("method does not exist")
	}
	ret := method.Call(nil)
	if len(ret) != 1 {
		return "", fmt.Errorf("method does not return 1 value")
	}
	return fmt.Sprintf("%v", ret[0].Interface()), nil
}
