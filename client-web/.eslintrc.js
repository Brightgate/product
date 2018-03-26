module.exports = {
    "root": true,
    "env": {
        "browser": true,
        "es6": true
    },
    "extends": [
        "eslint:recommended",
        // https://github.com/google/eslint-config-google
        "google",
        "plugin:vue/essential",
        "plugin:import/recommended",
    ],
    "parserOptions": {
        "sourceType": "module"
    },
    "rules": {
        // Code formatting rules-- above we adopt the 'google' ruleset, which
        // mostly governs style.  Here we modify it very slightly.

        // n.b. google disables this checker but work in eslint4 suggests
        // that it's OK to have it on.
        "indent": [
            "error",
            2
        ],
        // minor amendments relaxing google's rule
        "no-multi-spaces": [
            "error",
            {
                ignoreEOLComments: true,
                exceptions: { "Property": true }
            }
        ],
        // allow single-line functions
        "brace-style": ["error", "1tbs", { "allowSingleLine": true }],
        // In this case we're not building APIs for other modules, and our
        // top-level functions are utilities.  Hence this rule works poorly for
        // us and doesn't add value.
        "require-jsdoc": "off",

        // XXX For now we're not compliant with Google's choice of 80.
        "max-len": "off",
        // XXX We'd like to comply with camelCase new-cap but we don't have time to right now
        "camelcase": "off",
        "new-cap": "off",
        // We lower this to a warning because it's super annoying to have to fix
        // in the middle of doing something else.  However it should be kept clean.
        "no-trailing-spaces": "warn",

        // Below are our local choices, mostly around checkers for potential bugs
        // and language choices.
        // XXX We want to remove this so that console statements trigger
        // warnings.
        "no-console": "off",

        // these three rules are basically: eval is bad
        "no-eval": "error",
        "no-implied-eval": "error",
        "no-script-url": "error",

        "eqeqeq": ["error", "always"],
        "no-eq-null": "error",
        "no-self-compare": "error",
        "no-negated-condition": "error",
        "no-use-before-define": ["error", { "functions": false }],
        "prefer-template": "warn",
        "no-unneeded-ternary": "error",
        "guard-for-in": "error",
        "no-with": "error",

        // callbacks
        "prefer-arrow-callback": "error",
        "handle-callback-err": "warn",

        // variables
        // require let or const instead of var
        "no-var": "error",
        "no-label-var": "error",
        "prefer-const": "error",
        "no-shadow-restricted-names": "error",

        // The below section are rule settings cribbed from
        // https://github.com/xojs/eslint-config-xo/; those also
        // specified by eslint:recommended are filtered out.
        // xo has hundreds of rules, so we picked some.
        "for-direction": "error",
        "getter-return": "error",
        "no-await-in-loop": "error",
        "no-compare-neg-zero": "error",
        "no-cond-assign": "error",
        "no-constant-condition": "error",
        "no-control-regex": "error",
        "no-debugger": "error",
        "no-dupe-args": "error",
        "no-dupe-keys": "error",
        "no-duplicate-case": "error",
        "no-empty-character-class": "error",
        "no-empty": ["error", {
            allowEmptyCatch: true
        }],
        "no-extra-semi": "error",
        "no-prototype-builtins": "error",
        // warn when a regular string contains what looks like an ES6 template
        "no-template-curly-in-string": "error",
        "valid-typeof": ["error", {requireStringLiterals: false}],
        "accessor-pairs": "error",

        // Enforce some cleanliness in import statements
        "import/order": "error",
        "import/first": "error",
        "import/no-commonjs": "error",
        "import/prefer-default-export": "warn",
    }
};
