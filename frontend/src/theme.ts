import {
  createTheme,
  Paper,
  Button,
  TextInput,
  NumberInput,
  PasswordInput,
  Textarea,
  Select,
} from "@mantine/core";

export const theme = createTheme({
  primaryColor: "indigo",
  fontFamily: "ui-sans-serif, system-ui, -apple-system, Segoe UI, Roboto, sans-serif",
  fontFamilyMonospace: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
  defaultRadius: "xs",
  headings: { fontWeight: "600" },
  components: {
    Paper: Paper.extend({ defaultProps: { withBorder: true, p: "md", radius: "xs" } }),
    Button: Button.extend({ defaultProps: { radius: "xs" } }),
    TextInput: TextInput.extend({ defaultProps: { radius: "xs" } }),
    NumberInput: NumberInput.extend({ defaultProps: { radius: "xs" } }),
    PasswordInput: PasswordInput.extend({ defaultProps: { radius: "xs" } }),
    Textarea: Textarea.extend({ defaultProps: { radius: "xs" } }),
    Select: Select.extend({ defaultProps: { radius: "xs" } }),
  },
});
