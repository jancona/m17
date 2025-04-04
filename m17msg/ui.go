package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/jancona/m17text/m17"
)

type ui struct {
	servers, channels *widget.List
	messages          *fyne.Container
	messageScroll     *container.Scroll
	create            *widget.Entry
	win               fyne.Window

	data           *appData
	currentServer  *server
	currentChannel *channel
}

func (u *ui) appendMessages(list []*message) {
	items := u.messages.Objects
	for _, m := range list {
		items = append(items, newMessageCell(m))
	}
	u.messages.Objects = items
	u.messages.Refresh()
	u.messageScroll.ScrollToBottom()
}

func (u *ui) makeUI(w fyne.Window, a fyne.App) fyne.CanvasObject {
	u.servers = widget.NewList(
		func() int {
			if u.data == nil {
				return 1
			}
			return len(u.data.servers) + 1
		},
		func() fyne.CanvasObject {
			img := &canvas.Image{}
			img.SetMinSize(fyne.NewSize(theme.IconInlineSize()*2, theme.IconInlineSize()*2))
			return img
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if u.data == nil || id == len(u.data.servers) {
				o.(*canvas.Image).Resource = theme.ContentAddIcon()
			} else {
				o.(*canvas.Image).Resource = u.data.servers[id].icon()
			}
			o.Refresh()
		})
	u.servers.OnSelected = func(id widget.ListItemID) {
		if u.data == nil || id == len(u.data.servers) {
			u.servers.Unselect(id)
			u.addLogin(w, a)
			return
		}
		u.currentServer = u.data.servers[id]
		u.channels.Unselect(0)
		u.setChannel(nil)
		u.channels.Refresh()
	}

	u.channels = widget.NewList(
		func() int {
			if u.currentServer == nil {
				return 0
			}
			return len(u.currentServer.channels) + 1
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id == len(u.currentServer.channels) {
				o.(*widget.Label).SetText("Add New")
				return
			}
			o.(*widget.Label).SetText(u.currentServer.channels[id].name)
		})
	u.channels.OnSelected = func(id widget.ListItemID) {
		if id == len(u.currentServer.channels) {
			u.addChannel(w, a, func(ch *channel) widget.ListItemID {
				ch.server = u.currentServer
				u.currentServer.channels = append(u.currentServer.channels, ch)
				newCh := u.channels.CreateItem()
				newCh.(*widget.Label).SetText(ch.name)
				return id
			})
			return
		}
		u.setChannel(u.currentServer.channels[id])
	}
	u.channels.CreateItem() // The "Add New" item

	u.messages = container.NewVBox()
	u.messageScroll = container.NewScroll(u.messages)

	u.create = widget.NewEntry()
	u.create.OnSubmitted = u.send
	messagePane := container.NewBorder(nil,
		container.NewBorder(nil, nil, nil, widget.NewButtonWithIcon("",
			theme.MailSendIcon(), func() {
				u.send(u.create.Text)
			}), u.create), nil, nil, u.messageScroll)
	content := container.NewHSplit(u.channels, messagePane)
	content.Offset = 0.3
	return container.NewBorder(nil, nil, u.servers, nil, content)
}

func (u *ui) addChannel(w fyne.Window, a fyne.App, f func(*channel) widget.ListItemID) *channel {
	var ch *channel
	content, ce := u.channelContent(a)
	d := dialog.NewCustomConfirm("Channel to add", "OK", "Cancel",
		content, func(ok bool) {
			if !ok {
				return
			}
			var ch channel
			ch.name = m17.NormalizeCallsignModule(ce.Text)
			ch.id = ch.name
			id := f(&ch)
			u.setChannel(u.currentServer.channels[id])
			u.channels.Select(id)
			u.channels.Refresh()
		}, w)

	d.Resize(fyne.NewSize(375, 240))
	d.Show()
	return ch
}

func (u *ui) channelContent(a fyne.App) (fyne.CanvasObject, *widget.Entry) {
	channel := widget.NewEntry()
	f := widget.NewForm()
	f.AppendItem(&widget.FormItem{Text: "Channel", Widget: channel})
	return f, channel
}

func (u *ui) send(data string) {
	srv := u.currentServer.service
	if u.currentChannel == nil {
		return
	}
	srv.send(u.currentChannel, data)
	u.create.SetText("")
	ms, ok := u.currentChannel.server.service.(*m17Server)
	if ok {
		msg := &message{
			content: data,
			user: &user{
				name: ms.callsign,
			},
		}
		u.currentChannel.messages = append(u.currentChannel.messages, msg)
		u.appendMessages([]*message{msg})
	}
}

func (u *ui) setChannel(ch *channel) {
	var msgs []*message
	u.messages.Objects = nil

	if ch == nil {
		u.win.SetTitle(winTitle + " - " + u.currentServer.name)
	} else {
		u.win.SetTitle(winTitle + " - " + ch.server.name + " - " + ch.name)
		u.currentChannel = ch
		msgs = u.currentChannel.messages
	}

	u.appendMessages(msgs)
}
