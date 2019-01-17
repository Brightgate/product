
COPYRIGHT 2019 Brightgate Inc. All rights reserved.

This copyright notice is Copyright Management Information under 17 USC 1202
and is included to protect this work and deter copyright infringement.
Removal or alteration of this Copyright Management Information without the
express written permission of Brightgate Inc is prohibited, and any
such unauthorized removal or alteration will be a violation of federal law.


# README

The content used to generate product documentation is placed here, and then
manipulated by the build process to produce the various output formats
required.

## Requirements

Each document is a single HTML5 file.  Shared content between documents is not
supported in this implementation.

Interactive and continuous integration build machines are expected to have the
prerequisite packages needed for WeasyPrint.

## Invocations

Invoking

$ make doc

at the root of the source tree will create all of the documentation products.
These include a PDF print version, named `[filestem]-print.pdf`, and an HTML
fragment containing only the body content, named `[filestem]-body.html`.  At
present these outputs are placed in `doc/`, beside their respective source
files.

$ make doc-clobber

will remove all documentation products and intermediate files.

## Substitution points

Four markers are placed in the file, using HTML comments:

- The "head insertion" marker is expected to be substituted with `meta` and
  `link` elements that are appropriate for the output format.

    <!-- BRIGHTGATE HEAD INSERTION POINT -->

  For example, the insertion point might be replaced with CSS and JavaScript
  suitable for the brightgate.com website or CSS suitable for a PDF output
  engine.

- The "foot insertion" marker is expected to be substituted with footer
  content elements that are appropriate for the output format.  The marker is
  placed just prior to the final `body` element.

    <!-- BRIGHTGATE FOOT INSERTION POINT -->

  For example, the insertion point might be replaced with analytics support
  (like Google Analytics) and a site-wide footer on the brightgate.com website.

- The content start and end markers are expected to be used to prune the HTML
  document to only the content, and none of the document level elements.

    <!-- BRIGHTGATE CONTENT START -->
    <!-- BRIGHTGATE CONTENT END -->

  For example, the body content is extracted and then deposited into Framework
  7-compatible `div` elements.