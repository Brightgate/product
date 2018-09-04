/* eslint-disable import/no-commonjs */
const defaultPreset = require('cssnano-preset-default');

/*
 * Framework7 uses some ios specific calc() statements
 * which cssnano cannot understand.  See
 * https://github.com/framework7io/framework7/issues/2539
 */
module.exports = defaultPreset({
  calc: false,
});
