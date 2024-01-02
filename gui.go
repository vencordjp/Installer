//go:build !cli

/*
 * SPDX-License-Identifier: GPL-3.0
 * Vencord Installer, a cross platform gui/cli app for installing Vencord
 * Copyright (c) 2023 Vendicated and Vencord contributors
 */

package main

import (
	"bytes"
	_ "embed"
	"errors"
	"image"
	"image/color"
	"vencordinstaller/buildinfo"

	"github.com/AllenDang/imgui-go"

	// png decoder for icon
	_ "image/png"
	"os"
	path "path/filepath"
	"runtime"
	"strconv"
	"strings"
)

var (
	discords        []any
	radioIdx        int
	customChoiceIdx int

	customDir              string
	autoCompleteDir        string
	autoCompleteFile       string
	autoCompleteCandidates []string
	autoCompleteIdx        int
	lastAutoComplete       string
	didAutoComplete        bool

	modalId      = 0
	modalTitle   = "Oh No :("
	modalMessage = "You should never see this"

	acceptedOpenAsar   bool
	showedUpdatePrompt bool

	win *g.MasterWindow
)

//go:embed winres/icon.png
var iconBytes []byte

func init() {
	LogLevel = LevelDebug
}

func main() {
	InitGithubDownloader()
	discords = FindDiscords()

	customChoiceIdx = len(discords)

	go func() {
		<-GithubDoneChan
		g.Update()
	}()

	go func() {
		<-SelfUpdateCheckDoneChan
		g.Update()
	}()

	win = g.NewMasterWindow("VencordJP のインストール", 1200, 800, 0)

	icon, _, err := image.Decode(bytes.NewReader(iconBytes))
	if err != nil {
		Log.Warn("Failed to load application icon", err)
		Log.Debug(iconBytes, len(iconBytes))
	} else {
		win.SetIcon([]image.Image{icon})
	}
	win.Run(loop)
}

type CondWidget struct {
	predicate  bool
	ifWidget   func() g.Widget
	elseWidget func() g.Widget
}

func (w *CondWidget) Build() {
	if w.predicate {
		w.ifWidget().Build()
	} else if w.elseWidget != nil {
		w.elseWidget().Build()
	}
}

func getChosenInstall() *DiscordInstall {
	var choice *DiscordInstall
	if radioIdx == customChoiceIdx {
		choice = ParseDiscord(customDir, "")
		if choice == nil {
			g.OpenPopup("#invalid-custom-location")
		}
	} else {
		choice = discords[radioIdx].(*DiscordInstall)
	}
	return choice
}

func InstallLatestBuilds() (err error) {
	if IsDevInstall {
		return
	}

	err = installLatestBuilds()
	if err != nil {
		ShowModal("おっと。エラーが発生したようです。", "GitHubからVencordJPの最新ビルドをインストールできませんでした。詳細\n"+err.Error())
	}
	return
}

func handlePatch() {
	choice := getChosenInstall()
	if choice != nil {
		choice.Patch()
	}
}

func handleUnpatch() {
	choice := getChosenInstall()
	if choice != nil {
		choice.Unpatch()
	}
}

func handleOpenAsar() {
	if acceptedOpenAsar || getChosenInstall().IsOpenAsar() {
		handleOpenAsarConfirmed()
		return
	}

	g.OpenPopup("#openasar-confirm")
}

func handleOpenAsarConfirmed() {
	choice := getChosenInstall()
	if choice != nil {
		if choice.IsOpenAsar() {
			if err := choice.UninstallOpenAsar(); err != nil {
				handleErr(choice, err, "uninstall OpenAsar from")
			} else {
				g.OpenPopup("#openasar-unpatched")
				g.Update()
			}
		} else {
			if err := choice.InstallOpenAsar(); err != nil {
				handleErr(choice, err, "install OpenAsar on")
			} else {
				g.OpenPopup("#openasar-patched")
				g.Update()
			}
		}
	}
}

func handleErr(di *DiscordInstall, err error, action string) {
	if errors.Is(err, os.ErrPermission) {
		switch runtime.GOOS {
		case "windows":
			err = errors.New("権限がありません。(Permission denied)、Discordが完全に終了されていることを確認してください。(トレイからも)")
		case "darwin":
			// FIXME: This text is not selectable which is a bit mehhh
			command := "sudo chown -R \"${USER}:wheel\" " + di.path
			err = errors.New("Permission denied. Please grant the installer Full Disk Access in the system settings (privacy & security page).\n\nIf that also doesn't work, try running the following command in your terminal:\n" + command)
		default:
			err = errors.New("Permission denied. Maybe try running me as Administrator/Root?")
		}
	}

	ShowModal("Failed to "+action+" this Install", err.Error())
}

func HandleScuffedInstall() {
	g.OpenPopup("#scuffed-install")
}

func (di *DiscordInstall) Patch() {
	if CheckScuffedInstall() {
		return
	}
	if err := di.patch(); err != nil {
		handleErr(di, err, "patch")
	} else {
		g.OpenPopup("#patched")
	}
}

func (di *DiscordInstall) Unpatch() {
	if err := di.unpatch(); err != nil {
		handleErr(di, err, "unpatch")
	} else {
		g.OpenPopup("#unpatched")
	}
}

func onCustomInputChanged() {
	p := customDir
	if len(p) != 0 {
		// Select the custom option for people
		radioIdx = customChoiceIdx
	}

	dir := path.Dir(p)

	isNewDir := strings.HasSuffix(p, "/")
	wentUpADir := !isNewDir && dir != autoCompleteDir

	if isNewDir || wentUpADir {
		autoCompleteDir = dir
		// reset all the funnies
		autoCompleteIdx = 0
		lastAutoComplete = ""
		autoCompleteFile = ""
		autoCompleteCandidates = nil

		// Generate autocomplete items
		files, err := os.ReadDir(dir)
		if err == nil {
			for _, file := range files {
				autoCompleteCandidates = append(autoCompleteCandidates, file.Name())
			}
		}
	} else if !didAutoComplete {
		// reset auto complete and update our file
		autoCompleteFile = path.Base(p)
		lastAutoComplete = ""
	}

	if wentUpADir {
		autoCompleteFile = path.Base(p)
	}

	didAutoComplete = false
}

// go can you give me []any?
// to pass to giu RangeBuilder?
// yeeeeees
// actually returns []string like a boss
func makeAutoComplete() []any {
	input := strings.ToLower(autoCompleteFile)

	var candidates []any
	for _, e := range autoCompleteCandidates {
		file := strings.ToLower(e)
		if autoCompleteFile == "" || strings.HasPrefix(file, input) {
			candidates = append(candidates, e)
		}
	}
	return candidates
}

func makeRadioOnChange(i int) func() {
	return func() {
		radioIdx = i
	}
}

func renderFilesDirErr() g.Widget {
	return g.Layout{
		g.Dummy(0, 50),
		g.Style().
			SetColor(g.StyleColorText, DiscordRed).
			SetFontSize(30).
			To(
				g.Align(g.AlignCenter).To(
					g.Label("Error: Failed to create: "+FilesDirErr.Error()),
					g.Label("Resolve this error, then restart me!"),
				),
			),
	}
}

func Tooltip(label string) g.Widget {
	return g.Style().
		SetStyle(g.StyleVarWindowPadding, 10, 8).
		SetStyleFloat(g.StyleVarWindowRounding, 8).
		To(
			g.Tooltip(label),
		)
}

func InfoModal(id, title, description string) g.Widget {
	return RawInfoModal(id, title, description, false)
}

func RawInfoModal(id, title, description string, isOpenAsar bool) g.Widget {
	isDynamic := strings.HasPrefix(id, "#modal") && !strings.Contains(description, "\n")
	return g.Style().
		SetStyle(g.StyleVarWindowPadding, 30, 30).
		SetStyleFloat(g.StyleVarWindowRounding, 12).
		To(
			g.PopupModal(id).
				Flags(g.WindowFlagsNoTitleBar | Ternary(isDynamic, g.WindowFlagsAlwaysAutoResize, 0)).
				Layout(
					g.Align(g.AlignCenter).To(
						g.Style().SetFontSize(30).To(
							g.Label(title),
						),
						g.Style().SetFontSize(20).To(
							g.Label(description).Wrapped(isDynamic),
						),
						&CondWidget{id == "#scuffed-install", func() g.Widget {
							return g.Column(
								g.Dummy(0, 10),
								g.Button("Take me there!").OnClick(func() {
									// this issue only exists on windows so using Windows specific path is oki
									username := os.Getenv("USERNAME")
									programData := os.Getenv("PROGRAMDATA")
									g.OpenURL("file://" + path.Join(programData, username))
								}).Size(200, 30),
							)
						}, nil},
						g.Dummy(0, 20),
						&CondWidget{isOpenAsar,
							func() g.Widget {
								return g.Row(
									g.Button("承諾").
										OnClick(func() {
											acceptedOpenAsar = true
											g.CloseCurrentPopup()
										}).
										Size(100, 30),
									g.Button("キャンセル").
										OnClick(func() {
											g.CloseCurrentPopup()
										}).
										Size(100, 30),
								)
							},
							func() g.Widget {
								return g.Button("OK").
									OnClick(func() {
										g.CloseCurrentPopup()
									}).
									Size(100, 30)
							},
						},
					),
				),
		)
}

func UpdateModal() g.Widget {
	return g.Style().
		SetStyle(g.StyleVarWindowPadding, 30, 30).
		SetStyleFloat(g.StyleVarWindowRounding, 12).
		To(
			g.PopupModal("#update-prompt").
				Flags(g.WindowFlagsNoTitleBar | g.WindowFlagsAlwaysAutoResize).
				Layout(
					g.Align(g.AlignCenter).To(
						g.Style().SetFontSize(30).To(
							g.Label("Your Installer is outdated!"),
						),
						g.Style().SetFontSize(20).To(
							g.Label(
								"Would you like to update now?\n\n"+
									"Once you press Update Now, the new installer will automatically be downloaded.\n"+
									"The installer will temporarily seem unresponsive. Just wait!\n"+
									"Once the update is done, the Installer will automatically reopen.\n\n"+
									"On MacOs, Auto updates are not supported, so it will instead open in browser.",
							),
						),
						g.Row(
							g.Button("Update Now").
								OnClick(func() {
									if runtime.GOOS == "darwin" {
										g.CloseCurrentPopup()
										g.OpenURL(GetInstallerDownloadLink())
										return
									}

									err := UpdateSelf()
									g.CloseCurrentPopup()

									if err != nil {
										ShowModal("Failed to update self!", err.Error())
									} else {
										if err = RelaunchSelf(); err != nil {
											ShowModal("Failed to restart self! Please do it manually.", err.Error())
										}
									}
								}).
								Size(100, 30),
							g.Button("Later").
								OnClick(func() {
									g.CloseCurrentPopup()
								}).
								Size(100, 30),
						),
					),
				),
		)
}

func ShowModal(title, desc string) {
	modalTitle = title
	modalMessage = desc
	modalId++
	g.OpenPopup("#modal" + strconv.Itoa(modalId))
}

func renderInstaller() g.Widget {
	candidates := makeAutoComplete()
	wi, _ := win.GetSize()
	w := float32(wi) - 96

	var currentDiscord *DiscordInstall
	if radioIdx != customChoiceIdx {
		currentDiscord = discords[radioIdx].(*DiscordInstall)
	}
	var isOpenAsar = currentDiscord != nil && currentDiscord.IsOpenAsar()

	if CanUpdateSelf() && !showedUpdatePrompt {
		showedUpdatePrompt = true
		g.OpenPopup("#update-prompt")
	}

	layout := g.Layout{
		g.Dummy(0, 20),
		g.Separator(),
		g.Dummy(0, 5),

		g.Style().SetFontSize(20).To(
			renderErrorCard(
				DiscordYellow,
				"**GitHub**が安全なVencordJPの入手先です。それ以外のサイトは、悪質なファイルである可能性があります。\n"+
					"ほかのソースからダウンロードした場合は、Discordを削除/アンインストールし、Discordのパスワードを変更し、マルウェアスキャンを実行してください。",
				90,
			),
		),

		g.Dummy(0, 5),

		g.Style().SetFontSize(30).To(
			g.Label("パッチ先のインストールを選択"),
		),

		&CondWidget{len(discords) == 0, func() g.Widget {
			return g.Label("Discordのインストールが見つかりませんでした。Discordを最初にインストールしてください。")
		}, nil},

		g.Style().SetFontSize(20).To(
			g.RangeBuilder("Discords", discords, func(i int, v any) g.Widget {
				d := v.(*DiscordInstall)
				//goland:noinspection GoDeprecation
				text := strings.Title(d.branch) + " - " + d.path
				if d.isPatched {
					text += " [パッチ済み]"
				}
				return g.RadioButton(text, radioIdx == i).
					OnChange(makeRadioOnChange(i))
			}),

			g.RadioButton("カスタムのインストール場所", radioIdx == customChoiceIdx).
				OnChange(makeRadioOnChange(customChoiceIdx)),
		),

		g.Dummy(0, 5),
		g.Style().
			SetStyle(g.StyleVarFramePadding, 16, 16).
			SetFontSize(20).
			To(
				g.InputText(&customDir).Hint("カスタムの場所").
					Size(w - 16).
					Flags(g.InputTextFlagsCallbackCompletion).
					OnChange(onCustomInputChanged).
					// this library has its own autocomplete but it's broken
					Callback(
						func(data imgui.InputTextCallbackData) int32 {
							if len(candidates) == 0 {
								return 0
							}
							// just wrap around
							if autoCompleteIdx >= len(candidates) {
								autoCompleteIdx = 0
							}

							// used by change handler
							didAutoComplete = true

							start := len(customDir)
							// Delete previous auto complete
							if lastAutoComplete != "" {
								start -= len(lastAutoComplete)
								data.DeleteBytes(start, len(lastAutoComplete))
							} else if autoCompleteFile != "" { // delete partial input
								start -= len(autoCompleteFile)
								data.DeleteBytes(start, len(autoCompleteFile))
							}

							// Insert auto complete
							lastAutoComplete = candidates[autoCompleteIdx].(string)
							data.InsertBytes(start, []byte(lastAutoComplete))
							autoCompleteIdx++

							return 0
						},
					),
			),
		g.RangeBuilder("AutoComplete", candidates, func(i int, v any) g.Widget {
			dir := v.(string)
			return g.Label(dir)
		}),

		g.Dummy(0, 20),

		g.Style().SetFontSize(20).To(
			g.Row(
				g.Style().
					SetColor(g.StyleColorButton, DiscordGreen).
					SetDisabled(GithubError != nil).
					To(
						g.Button("インストール").
							OnClick(handlePatch).
							Size((w-40)/4, 50),
						Tooltip("選択したDiscordのインストールをパッチします。"),
					),
				g.Style().
					SetColor(g.StyleColorButton, DiscordBlue).
					SetDisabled(GithubError != nil).
					To(
						g.Button("再インストールと修復").
							OnClick(func() {
								if IsDevInstall {
									handlePatch()
								} else {
									err := InstallLatestBuilds()
									if err == nil {
										handlePatch()
									}
								}
							}).
							Size((w-40)/4, 50),
						Tooltip("VencordJPを再インストールし、更新します。"),
					),
				g.Style().
					SetColor(g.StyleColorButton, DiscordRed).
					To(
						g.Button("アンインストール").
							OnClick(handleUnpatch).
							Size((w-40)/4, 50),
						Tooltip("選択したDiscordのインストールからVencordJPをアンインストールします。"),
					),
				g.Style().
					SetColor(g.StyleColorButton, Ternary(isOpenAsar, DiscordRed, DiscordGreen)).
					To(
						g.Button(Ternary(isOpenAsar, "OpenAsarをアンインストール", Ternary(currentDiscord != nil, "OpenAsarをインストール", "OpenAsarを(アン)インストール"))).
							OnClick(handleOpenAsar).
							Size((w-40)/4, 50),
						Tooltip("OpenAsarを管理"),
					),
			),
		),

		InfoModal("#patched", "正常にインストールされました", "Discordがまだ開いている場合、Discordを閉じてください。\n"+
			"Discordを起動したら、Discordの設定にVencordのカテゴリが存在することを確認してください。"),
		InfoModal("#unpatched", "正常にアンインストールされました", "Discordがまだ開いている場合、Discordを閉じてください。"),
		InfoModal("#scuffed-install", "確認してください", "あなたは壊れているDiscordのインストールを所持しています:\n"+
			"Discordが何らかの理由で間違った場所にインストールすることがあります。\n"+
			"パッチを当てる前にこれを修正しなければ、VencordJPは機能しない可能性が高いです。\n\n"+
			"下のボタンを使ってそこにジャンプし、DiscordまたはSquirrelと呼ばれるフォルダを削除してください。\n"+
			"フォルダが空になった場合は、戻ってそのフォルダも削除してください。\n"+
			"その後、Discordが起動するか確認してください。起動しない場合は、再インストールしてください。"),
		RawInfoModal("#openasar-confirm", "OpenAsar", "OpenAsarは、Discordデスクトップのapp.asarに代わるオープンソースのapp.asarです。\n"+
			"Vencord及びVencordJPはOpenAsarとは一切関係ありません。\n"+
			"OpenAsarのインストールは自己責任で行ってください。もし、OpenAsarで問題が発生した場合、\n"+
			"OpenAsarのサーバーに参加してください。\n\n"+
			"OpenAsarをインストールするには、承諾を押し、もう一度「OpenAsarをインストール」をクリックします。", true),
		InfoModal("#openasar-patched", "正常にOpenAsarをインストールしました", "Discordがまだ開いている場合、Discordを閉じてください。\nDiscordの設定にOpenAsarが存在することを確認してください。"),
		InfoModal("#openasar-unpatched", "正常にOpenAsarをアンインストールしました", "Discordがまだ開いている場合、Discordを閉じてください。"),
		InfoModal("#invalid-custom-location", "無効な場所", "指定した場所に有効なDiscordのインストールが見つかりませんでした。パスを確認してください。"),
		InfoModal("#modal"+strconv.Itoa(modalId), modalTitle, modalMessage),

		UpdateModal(),
	}

	return layout
}

func renderErrorCard(col color.Color, message string, height float32) g.Widget {
	return g.Style().
		SetColor(g.StyleColorChildBg, col).
		SetStyleFloat(g.StyleVarAlpha, 0.9).
		SetStyle(g.StyleVarWindowPadding, 10, 10).
		SetStyleFloat(g.StyleVarChildRounding, 5).
		To(
			g.Child().
				Size(g.Auto, height).
				Layout(
					g.Row(
						g.Style().SetColor(g.StyleColorText, color.Black).To(
							g.Markdown(&message),
						),
					),
				),
		)
}

func loop() {
	g.PushWindowPadding(48, 48)

	g.SingleWindow().
		RegisterKeyboardShortcuts(
			g.WindowShortcut{Key: g.KeyUp, Callback: func() {
				if radioIdx > 0 {
					radioIdx--
				}
			}},
			g.WindowShortcut{Key: g.KeyDown, Callback: func() {
				if radioIdx < customChoiceIdx {
					radioIdx++
				}
			}},
		).
		Layout(
			g.Align(g.AlignCenter).To(
				g.Style().SetFontSize(40).To(
					g.Label("VencordJP インストーラー"),
				),
			),

			g.Dummy(0, 20),
			g.Style().SetFontSize(20).To(
				g.Row(
					g.Label(Ternary(IsDevInstall, "開発インストール: ", "ファイルはここへダウンロードされます: ")+FilesDir),
					g.Style().
						SetColor(g.StyleColorButton, DiscordBlue).
						SetStyle(g.StyleVarFramePadding, 4, 4).
						To(
							g.Button("Open Directory").OnClick(func() {
								g.OpenURL("file://" + FilesDir)
							}),
						),
				),
				&CondWidget{!IsDevInstall, func() g.Widget {
					return g.Label("インストール場所をカスタマイズするには、環境変数「VENCORD_USER_DATA_DIR」をパスにしてインストーラーを再起動してください。").Wrapped(true)
				}, nil},
				g.Dummy(0, 10),
				g.Label("インストーラーバージョン: "+buildinfo.InstallerTag+" ("+buildinfo.InstallerGitHash+")"+Ternary(IsSelfOutdated, " - 古い", "")),
				g.Label("ローカルのVencordJPバージョン: "+InstalledHash),
				&CondWidget{
					GithubError == nil,
					func() g.Widget {
						if IsDevInstall {
							return g.Label("VencordJPは開発者モードのため、アップデートされません。")
						}
						return g.Label("最新のVencordJPバージョン: " + LatestHash)
					}, func() g.Widget {
						return renderErrorCard(DiscordRed, "ハッシュをGitHubから取得できませんでした。詳細: "+GithubError.Error(), 40)
					},
				},
			),

			&CondWidget{
				predicate:  FilesDirErr != nil,
				ifWidget:   renderFilesDirErr,
				elseWidget: renderInstaller,
			},
		)

	g.PopStyle()
}
