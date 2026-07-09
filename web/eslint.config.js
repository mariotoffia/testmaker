import reactHooks from "eslint-plugin-react-hooks";
import reactRefresh from "eslint-plugin-react-refresh";
import tseslint from "typescript-eslint";

// ESLint 9 flat config. typescript-eslint's recommended set brings the parser
// and disables core rules TS already covers (e.g. no-undef); react-hooks guards
// the dependency-array footguns and react-refresh keeps modules HMR-friendly.
export default tseslint.config(
  { ignores: ["dist"] },
  ...tseslint.configs.recommended,
  {
    files: ["**/*.{ts,tsx}"],
    plugins: {
      "react-hooks": reactHooks,
      "react-refresh": reactRefresh,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      "react-refresh/only-export-components": ["warn", { allowConstantExport: true }],
    },
  },
);
