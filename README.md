# Polysender

Send email and SMS broadcasts to your contacts.

*Polysender* is a desktop application, so it does not require a complicated server setup.

Emails are sent via an SMTP service of your choice.

SMS are sent via your *Android* phone connected to your computer with the help of [third party software](#third-party-software).

![Demo video](../media/demo.webp?raw=true)

## Supported platforms

It should work on *Linux*, *Windows 8.1+*, *macOS 10.13+*, although it has only been tested on *Linux* and *macOS 11*.

Sending SMS via *ADB* is supported on *Android* versions from 5 to 10 but it might not work on some versions since it hasn't been tested.
*Android 11* is not supported yet.

## Warning

This is alpha quality software released for testing purposes. Not recommended for production use. Bug reports are appreciated.

You are responsible for compliance with laws and regulations regarding electronic communications, and your carrier's terms of service.

## Installation

### Option 1: Download release binary (recommended)

Download the latest [release](https://github.com/cmitsakis/polysender/releases) and run it. No installation is required.

#### macOS

On *macOS* you have to remove the application from *quarantine* by following the instructions [here](https://support.apple.com/guide/mac-help/welcome/mac), or by running the following command:

```sh
xattr -d com.apple.quarantine /path/to/polysender
```

### Option 2: Build from source

If you have installed the compile-time requirements
([Go](https://golang.org/) and [Fyne prerequisites](https://developer.fyne.io/started/#prerequisites)),
you can install *Polysender* using the following command:

```sh
go install go.polysender.org/cmd/polysender@latest
```

### Third-party software

In order to send SMS, you have to install *ADB*.

*ADB* might not work on all *Android* versions.
It requires your phone is connected over USB and has USB debugging enabled.

Alternatively you can install *KDE Connect* but it currently doesn't work. It's included for testing purposes only.

#### ADB (recommended)

1. [download](https://developer.android.com/studio/releases/platform-tools#downloads) and install *ADB*
2. start *ADB*
3. connect your *Android* phone to your computer via USB
4. enable developer options and USB debugging on your phone
   - [instructions](https://developer.android.com/studio/debug/dev-options) for most *Android* phones
   - [instructions](https://help.airdroid.com/hc/en-us/articles/360045329413-How-to-Enable-USB-debugging-on-Xiaomi-) for *Xiaomi* phones

#### KDE Connect (for testing only)

1. [download](https://kdeconnect.kde.org/download.html) and install *KDE Connect*.
   Windows users can also install it from the [Microsoft store](https://www.microsoft.com/store/apps/9N93MRMSXBF0).
2. install the *KDE Connect* [app](https://play.google.com/store/apps/details?id=org.kde.kdeconnect_tp) on your *Android* phone
3. [pair](https://userbase.kde.org/KDEConnect#Pairing_two_devices_together) your phone with your computer

*KDE Connect* does not support *macOS*.

## Contributing

### Reporting bugs

Good bug reports are the most valuable contribution, if they include enough information to reproduce the bug.

Please search existing issues before opening a new one. If an issue already exists, you can add more information.

Your bug reports should include:

1. steps to reproduce the bug (this is very important!)
2. what you expected to happen
3. what actually happens
4. screenshots or logs if possible (make sure to remove any personally identifiable information like emails, phones)
5. the version you are using (including *ADB* or *KDE Connect* version if applicable)
6. your operating system

### Contributing code

- Create small PRs addressing a single issue
- Maintain clean commit history with atomic commits
- Mention the issue number in your commit messages
- Major changes and new features should be discussed first

#### Contributions license

Your contributions must be licensed under the terms of the *Blue Oak Model License 1.0.0* (see [License](#license)).

## License

Copyright (C) 2021 Charalampos Mitsakis

*Polysender* is licensed under the terms of the [PolyForm Internal Use License 1.0.0](LICENSE-PolyForm-Internal-Use.md).
This is a source-available license that permits use and modifications for internal business purposes only, while prohibiting distribution, SaaS, and service bureau use.

Third-party contributions are licensed under the terms of the [Blue Oak Model License 1.0.0](LICENSE-BlueOak.md).

As additional permission, you are allowed to contribute your modifications back as pull requests to [this repository](https://github.com/cmitsakis/polysender),
provided that your contributions are licensed under the terms of the *Blue Oak Model License 1.0.0*.
By submitting a pull request you certify that you can and do license your contribution under the terms of the *Blue Oak Model License 1.0.0*.
Publicly maintaining an independent fork is not permitted.
This additional permission does not allow you to modify the license, the additional permission, or the copyright notice.
