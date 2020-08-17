/*
 * Copyright 2018 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


import {format, formatRelative, formatDistance, formatDistanceStrict, parseISO} from 'date-fns';
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

function _formatDistance(date, baseDate, options) {
  return formatDistance(date, baseDate,
    Object.assign(getLocale(), options));
}

function _formatDistanceStrict(date, baseDate, options) {
  return formatDistanceStrict(date, baseDate,
    Object.assign(getLocale(), options));
}

export {
  _format as format,
  _formatDistance as formatDistance,
  _formatDistanceStrict as formatDistanceStrict,
  _formatRelative as formatRelative,
  parseISO,
};

