/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

import {format, formatRelative} from 'date-fns';
import {enUS, de} from 'date-fns/locale';

const locales = {
  'en': enUS,
  'en-us': enUS,
  'de': de,
};

/*
 * As recommended by https://date-fns.org/v2.0.0-alpha.16/docs/I18n
 * we write thin shims around the date-fns routines which take locale
 * parameters.
 */
function getLocale() {
  let locale = locales[window.__b10e_locale__];
  if (locale === undefined) {
    locale = locales['en'];
  }
  return {locale: locale};
}

function _format(date, formatStr, options) {
  return format(date, formatStr,
    Object.assign(getLocale(), options));
}

function _formatRelative(date, baseDate, options) {
  return formatRelative(date, baseDate,
    Object.assign(getLocale(), options));
}

export {_format as format, _formatRelative as formatRelative};
