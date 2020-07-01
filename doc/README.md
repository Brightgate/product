
COPYRIGHT 2019 Brightgate Inc. All rights reserved.

This copyright notice is Copyright Management Information under 17 USC 1202
and is included to protect this work and deter copyright infringement.
Removal or alteration of this Copyright Management Information without the
express written permission of Brightgate Inc is prohibited, and any
such unauthorized removal or alteration will be a violation of federal law.


# README

The content used to generate product documentation is placed in `doc/src`, and
then manipulated by the build process, using `doc/build` for intermediate
processing.  The build places artifacts in `doc/output`.  The client-web
subsystem scoops up intermediate build products from `doc/build`.

## Requirements

Each document is a single HTML5 file.  Shared content between documents is not
supported in this implementation.

Interactive and continuous integration build machines are expected to have the
prerequisite packages needed for WeasyPrint.

## Invocations

Invoking

```
$ make doc
```

at the root of the source tree will create all of the documentation products.
These include a PDF print version, named `[filestem]-print.pdf`, and an HTML
fragment containing only the body content, named `[filestem]-body.html`.
These outputs are placed in `doc/output`.

```
$ make doc-check
```

at the root of the source tree will run sanity checks over the documentation
HTML, looking for syntactic problems.  Documentation must pass these checks.

```
$ make doc-clobber
```

will remove all documentation products and intermediate files.

## Substitution points

Four markers are placed in the file, using HTML comments:

- The "head insertion" marker is expected to be substituted with `meta` and
  `link` elements that are appropriate for the output format.

    `<!-- BRIGHTGATE HEAD INSERTION POINT -->`

  For example, the insertion point might be replaced with CSS and JavaScript
  suitable for the brightgate.com website or CSS suitable for a PDF output
  engine.

- The "foot insertion" marker is expected to be substituted with footer
  content elements that are appropriate for the output format.  The marker is
  placed just prior to the final `body` element.

    `<!-- BRIGHTGATE FOOT INSERTION POINT -->`

  For example, the insertion point might be replaced with analytics support
  (like Google Analytics) and a site-wide footer on the brightgate.com website.

- The content start and end markers are expected to be used to prune the HTML
  document to only the content, and none of the document level elements.

    `<!-- BRIGHTGATE CONTENT START -->`
    `<!-- BRIGHTGATE CONTENT END -->`

  For example, the body content is extracted and then deposited into Framework
  7-compatible `div` elements.

## Screenshots

Many of the images in the documentation are screenshots.  Screenshots should be
saved as PNG images.  The documentation build applies some tools to reduce the
size of these images.

For the web application (and otherwise when possible) use Google Chrome
developer tools to make screenshots for inclusion in the documentation.
Typically we put the view into mobile mode, with "Pixel 2" selected as the
target device.  To take a screenshot, use the Three-dots Menu > Capture
Screenshot (not Capture full-size screenshot).  Then use tools such as Apple
Preview to crop the screenshot if needed.  Make sure to take a look at the
image to make sure no developer artifacts (such as highlighted elements or
labeled elements) are present, sometimes Chrome seems to mess up and emit
screenshots with those included.

Screenshots need not have borders, and in general it is better that they don't,
as the doc css rules automatically includes them.
