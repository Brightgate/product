<!--
Copyright 2018 Brightgate Inc.

This Source Code Form is subject to the terms of the Mozilla Public
License, v. 2.0. If a copy of the MPL was not distributed with this
file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->

## Asset Generation

Logo assets were generated using ImageMagick as follows:

```shellsession
$ convert -trim 'bglogo_blueArtboard 1@4x (1).png' bglogo_blue_trim.png
$ convert -trim 'bglogo_WhiteNoBackArtboard 1@4x.png' bglogo_white_trim.png
$ convert -resize x36 bglogo_blue_trim.png bglogo_navbar_ios.png
$ convert -resize x36 bglogo_white_trim.png bglogo_navbar_md.png
$ convert -resize x72 bglogo_white_trim.png bglogo_navbar_md@2x.png
$ convert -resize x72 bglogo_blue_trim.png bglogo_navbar_ios@2x.png
```

The icons in `devid/` were generated using IconJar as follows:

- Select all icons, then Export Selection

| SIZE  | PREFIX | SUFFIX  | TYPE | FILL
| ----- | ------ | ------- | ---- | -------
| 64x64 | None   | -active | PNG  | #6CC04A
| 64x64 | None   | None    | PNG  | #626262

- Once all icons are rendered to PNG, use ImageOptim on MacOS to reduce the PNG
  size (reduces PNG size by about 75%).

- The source .iconjar file is in [`client-web/src/ui-icons.iconjar.zip`](../../src/ui-icons.iconjar.zip).
