```
COPYRIGHT 2018 Brightgate Inc. All rights reserved.

This copyright notice is Copyright Management Information under 17 USC 1202
and is included to protect this work and deter copyright infringement.
Removal or alteration of this Copyright Management Information without the
express written permission of Brightgate Inc is prohibited, and any
such unauthorized removal or alteration will be a violation of federal law.
```

Logo assets were generated using ImageMagick as follows:

```
convert -trim 'bglogo_blueArtboard 1@4x (1).png' bglogo_blue_trim.png
convert -trim 'bglogo_WhiteNoBackArtboard 1@4x.png' bglogo_white_trim.png
convert -resize x36 bglogo_blue_trim.png bglogo_navbar_ios.png
convert -resize x36 bglogo_white_trim.png bglogo_navbar_md.png
convert -resize x72 bglogo_white_trim.png bglogo_navbar_md@2x.png
convert -resize x72 bglogo_blue_trim.png bglogo_navbar_ios@2x.png
```

The icons in `devid/` were generated using IconJar as follows:

- Select all icons, then Export Selection
```
  SIZE    PREFIX   SUFFIX   TYPE   FILL
  64x64   None     -active  PNG    #6CC04A
  64x64   None     None     PNG    #626262
```

- Once all icons are rendered to PNG, use ImageOptim on MacOS to reduce the PNG
  size (reduces PNG size by about 75%).

- The source .iconjar file is in client-web/src/ui-icons.iconjar.zip
