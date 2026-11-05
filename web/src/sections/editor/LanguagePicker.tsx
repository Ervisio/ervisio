import { languages } from '@codemirror/language-data';
export default function LanguagePicker({
  value,
  onChange,
  label,
  automatic,
  plain,
}: {
  value: string;
  onChange(value: string): void;
  label: string;
  automatic: string;
  plain: string;
}) {
  return (
    <select aria-label={label} value={value} onChange={(e) => onChange(e.target.value)}>
      <option value="auto">{automatic}</option>
      <option value="plain">{plain}</option>
      {languages.map((language) => (
        <option key={language.name} value={language.name}>
          {language.name}
        </option>
      ))}
    </select>
  );
}
