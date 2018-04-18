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
