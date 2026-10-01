import {
  forwardRef, useEffect, useId, useRef,
  type InputHTMLAttributes, type KeyboardEvent, type ReactNode, type SelectHTMLAttributes, type TextareaHTMLAttributes,
} from 'react';
import { Icon, type IconName } from './Icon';

/* ---------- Field wrapper ---------- */
export interface FieldProps {
  label?: ReactNode;
  hint?: ReactNode;
  error?: ReactNode;
  htmlFor?: string;
  children: ReactNode;
  className?: string;
}
export function Field({ label, hint, error, htmlFor, children, className }: FieldProps) {
  return (
    <div className={`ui-field${error ? ' ui-field--bad' : ''}${className ? ' ' + className : ''}`}>
      {label && <label htmlFor={htmlFor}>{label}</label>}
      {children}
      {(error || hint) && <span className="ui-hint" id={htmlFor ? `${htmlFor}-hint` : undefined}>{error || hint}</span>}
    </div>
  );
}

/* ---------- Input ---------- */
export interface InputProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'size'> {
  label?: ReactNode;
  hint?: ReactNode;
  error?: ReactNode;
  icon?: IconName;
  /** Keyboard shortcut hint on the right, e.g. "Ctrl K". */
  kbd?: string;
  /** Extra element on the right (e.g. a show/hide IconButton). */
  end?: ReactNode;
  mono?: boolean;
  compact?: boolean;
  fieldClassName?: string;
}
export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  { label, hint, error, icon, kbd, end, mono, compact, fieldClassName, id, className, ...rest },
  ref,
) {
  const uid = useId();
  const iid = id ?? uid;
  return (
    <Field label={label} hint={hint} error={error} htmlFor={iid} className={fieldClassName}>
      <div className={`ui-in${mono ? ' ui-in--mono' : ''}${compact ? ' ui-in--compact' : ''}${className ? ' ' + className : ''}`}>
        {icon && <Icon name={icon} />}
        <input ref={ref} id={iid} aria-invalid={error ? true : undefined} aria-describedby={error || hint ? `${iid}-hint` : undefined} {...rest} />
        {kbd && <kbd className="ui-kbd">{kbd}</kbd>}
        {end}
      </div>
    </Field>
  );
});

export interface TextareaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
  label?: ReactNode;
  hint?: ReactNode;
  error?: ReactNode;
  mono?: boolean;
}
export const Textarea = forwardRef<HTMLTextAreaElement, TextareaProps>(function Textarea({ label, hint, error, mono, id, ...rest }, ref) {
  const uid = useId();
  const iid = id ?? uid;
  return (
    <Field label={label} hint={hint} error={error} htmlFor={iid}>
      <div className={`ui-in ui-in--textarea${mono ? ' ui-in--mono' : ''}`}>
        <textarea ref={ref} id={iid} aria-invalid={error ? true : undefined} {...rest} />
      </div>
    </Field>
  );
});

/* ---------- Select (native, styled) ---------- */
export interface SelectOption {
  value: string;
  label: string;
  disabled?: boolean;
}
export interface SelectProps extends Omit<SelectHTMLAttributes<HTMLSelectElement>, 'onChange' | 'value'> {
  label?: ReactNode;
  hint?: ReactNode;
  error?: ReactNode;
  options: SelectOption[];
  value: string;
  onChange(value: string): void;
  compact?: boolean;
  fieldClassName?: string;
}
export function Select({ label, hint, error, options, value, onChange, compact, fieldClassName, id, ...rest }: SelectProps) {
  const uid = useId();
  const iid = id ?? uid;
  return (
    <Field label={label} hint={hint} error={error} htmlFor={iid} className={fieldClassName}>
      <div className={`ui-in ui-in--select${compact ? ' ui-in--compact' : ''}`}>
        <select id={iid} value={value} onChange={(e) => onChange(e.target.value)} {...rest}>
          {options.map((o) => (
            <option key={o.value} value={o.value} disabled={o.disabled}>
              {o.label}
            </option>
          ))}
        </select>
        <Icon name="chevron" />
      </div>
    </Field>
  );
}

/* ---------- Switch ---------- */
export interface SwitchProps {
  checked: boolean;
  onChange(checked: boolean): void;
  label?: ReactNode;
  disabled?: boolean;
  'aria-label'?: string;
  id?: string;
}
export function Switch({ checked, onChange, label, disabled, id, ...aria }: SwitchProps) {
  const btn = (
    <button
      type="button"
      role="switch"
      id={id}
      aria-checked={checked}
      disabled={disabled}
      className="ui-switch"
      onClick={() => onChange(!checked)}
      {...aria}
    />
  );
  return label ? (
    <span className="ui-choice">
      {btn}
      <span onClick={() => !disabled && onChange(!checked)}>{label}</span>
    </span>
  ) : (
    btn
  );
}

/* ---------- Checkbox / Radio ---------- */
export interface CheckboxProps {
  checked: boolean;
  onChange(checked: boolean): void;
  label?: ReactNode;
  disabled?: boolean;
  indeterminate?: boolean;
  'aria-label'?: string;
}
export function Checkbox({ checked, onChange, label, disabled, indeterminate, ...aria }: CheckboxProps) {
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (ref.current) ref.current.indeterminate = !!indeterminate;
  }, [indeterminate]);
  return (
    <label className={`ui-choice${disabled ? ' ui-choice--disabled' : ''}`}>
      <span className="ui-check">
        <input ref={ref} type="checkbox" checked={checked} disabled={disabled} onChange={(e) => onChange(e.target.checked)} {...aria} />
        <i><Icon name={indeterminate ? 'minus' : 'check'} /></i>
      </span>
      {label}
    </label>
  );
}

export interface RadioProps {
  checked: boolean;
  onChange(): void;
  label?: ReactNode;
  name?: string;
  value?: string;
  disabled?: boolean;
}
export function Radio({ checked, onChange, label, name, value, disabled }: RadioProps) {
  return (
    <label className={`ui-choice${disabled ? ' ui-choice--disabled' : ''}`}>
      <span className="ui-radio">
        <input type="radio" name={name} value={value} checked={checked} disabled={disabled} onChange={onChange} />
        <i />
      </span>
      {label}
    </label>
  );
}

/* ---------- Segmented ---------- */
export interface SegmentedOption<T extends string> {
  value: T;
  label?: ReactNode;
  icon?: IconName;
  /** Accessible name for icon-only options. */
  title?: string;
}
export interface SegmentedProps<T extends string> {
  options: SegmentedOption<T>[];
  value: T;
  onChange(v: T): void;
  'aria-label'?: string;
}
export function Segmented<T extends string>({ options, value, onChange, ...aria }: SegmentedProps<T>) {
  const onKey = (e: KeyboardEvent<HTMLDivElement>) => {
    const i = options.findIndex((o) => o.value === value);
    let n = -1;
    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') n = (i + 1) % options.length;
    if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') n = (i - 1 + options.length) % options.length;
    if (n >= 0) {
      e.preventDefault();
      onChange(options[n].value);
      (e.currentTarget.children[n] as HTMLElement | undefined)?.focus();
    }
  };
  return (
    <div className="ui-seg" role="radiogroup" onKeyDown={onKey} {...aria}>
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={o.value === value}
          aria-label={o.title}
          title={o.title}
          tabIndex={o.value === value ? 0 : -1}
          onClick={() => onChange(o.value)}
        >
          {o.icon && <Icon name={o.icon} />}
          {o.label}
        </button>
      ))}
    </div>
  );
}
