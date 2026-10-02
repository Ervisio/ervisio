/**
 * Typed wrappers for the app's component kit (sdk.ui). The kit exists only at activate time, so each wrapper looks
 * the real component up when it renders. Import from here: `import { Button, Dialog, toast } from '../kit'`.
 * Prop types mirror web/src/ui (kept loose where the app's types are generic).
 */
import { createElement } from 'react';
import type { ComponentType, CSSProperties, ChangeEvent, FormEvent, InputHTMLAttributes, MouseEvent, ReactElement, ReactNode } from 'react';
import { getSdk } from './sdk';

function wrap<P>(name: string): ComponentType<P> {
  const C = (props: P) => createElement(getSdk().ui[name], props as Record<string, unknown>);
  C.displayName = name;
  return C as ComponentType<P>;
}

export type HueId = 'ov' | 'term' | 'file' | 'log' | 'svc' | 'sw' | 'usr' | 'plg';
export type Tone = 'ok' | 'warn' | 'err' | 'info' | 'neutral';

export interface ButtonProps {
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger' | 'danger-solid';
  icon?: string;
  iconOnly?: boolean;
  loading?: boolean;
  size?: 'sm' | 'md' | 'lg';
  block?: boolean;
  disabled?: boolean;
  type?: 'button' | 'submit';
  className?: string;
  title?: string;
  'aria-label'?: string;
  onClick?(e: MouseEvent<HTMLButtonElement>): void;
  children?: ReactNode;
}
export const Button = wrap<ButtonProps>('Button');

export interface IconButtonProps extends Omit<ButtonProps, 'icon' | 'children' | 'iconOnly'> {
  icon: string;
  /** Accessible name, also shown as tooltip. */
  label: string;
  tooltip?: boolean;
}
export const IconButton = wrap<IconButtonProps>('IconButton');

export interface IconProps {
  name: string;
  size?: number;
  className?: string;
  style?: CSSProperties;
}
export const Icon = wrap<IconProps>('Icon');

export interface InputProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'size' | 'onChange'> {
  label?: ReactNode;
  hint?: ReactNode;
  error?: ReactNode;
  icon?: string;
  kbd?: string;
  end?: ReactNode;
  mono?: boolean;
  compact?: boolean;
  fieldClassName?: string;
  onChange?(e: ChangeEvent<HTMLInputElement>): void;
}
export const Input = wrap<InputProps>('Input');

export interface TextareaProps {
  label?: ReactNode;
  hint?: ReactNode;
  error?: ReactNode;
  mono?: boolean;
  rows?: number;
  value?: string;
  placeholder?: string;
  onChange?(e: ChangeEvent<HTMLTextAreaElement>): void;
  [k: string]: unknown;
}
export const Textarea = wrap<TextareaProps>('Textarea');

export interface SelectOption {
  value: string;
  label: ReactNode;
  disabled?: boolean;
}
export interface SelectProps {
  label?: ReactNode;
  hint?: ReactNode;
  error?: ReactNode;
  options: SelectOption[];
  value: string;
  onChange(value: string): void;
  compact?: boolean;
  disabled?: boolean;
}
export const Select = wrap<SelectProps>('Select');

export interface FieldProps {
  label?: ReactNode;
  hint?: ReactNode;
  error?: ReactNode;
  htmlFor?: string;
  className?: string;
  children?: ReactNode;
}
export const Field = wrap<FieldProps>('Field');

export interface SwitchProps {
  checked: boolean;
  onChange(checked: boolean): void;
  label?: ReactNode;
  disabled?: boolean;
  'aria-label'?: string;
}
export const Switch = wrap<SwitchProps>('Switch');

export interface CheckboxProps {
  checked: boolean;
  onChange(checked: boolean): void;
  label?: ReactNode;
  disabled?: boolean;
  indeterminate?: boolean;
  'aria-label'?: string;
}
export const Checkbox = wrap<CheckboxProps>('Checkbox');

export interface SegmentedOption<T extends string = string> {
  value: T;
  label?: ReactNode;
  icon?: string;
  title?: string;
}
export const Segmented = wrap<{ options: SegmentedOption[]; value: string; onChange(v: string): void; 'aria-label'?: string }>('Segmented');

export const Badge = wrap<{ tone?: Tone; dot?: boolean; className?: string; children: ReactNode }>('Badge');
export const Chip = wrap<{ pressed?: boolean; onClick?(): void; icon?: string; count?: number | string; hue?: HueId; children: ReactNode }>('Chip');
export const Progress = wrap<{ value?: number; tone?: 'ok' | 'warn' | 'err'; hue?: HueId; label?: string }>('Progress');
export const Skeleton = wrap<{ width?: number | string; height?: number | string; lines?: number; style?: CSSProperties }>('Skeleton');
export const EmptyState = wrap<{ icon?: string; title: ReactNode; text?: ReactNode; action?: ReactNode; hue?: HueId }>('EmptyState');
export const Card = wrap<{ title?: ReactNode; icon?: string; action?: ReactNode; hue?: HueId; surface?: boolean; className?: string; style?: CSSProperties; children?: ReactNode }>('Card');
export const StatCard = wrap<{ hue: HueId; icon: string; label: ReactNode; value: ReactNode; unit?: string; sub?: ReactNode; percent?: number; spark?: number[]; onClick?(): void }>('StatCard');

export interface TabItem {
  id: string;
  label: ReactNode;
  icon?: string;
  count?: number | string;
}
export const Tabs = wrap<{ items: TabItem[]; value: string; onChange(id: string): void; variant?: 'pill' | 'underline'; hue?: HueId; 'aria-label'?: string; className?: string }>('Tabs');

export interface DialogProps {
  open: boolean;
  onClose(): void;
  title: ReactNode;
  description?: ReactNode;
  icon?: string;
  tone?: 'acc' | 'err' | 'warn';
  children?: ReactNode;
  footer?: ReactNode;
  size?: 'md' | 'lg';
  dismissable?: boolean;
  role?: 'dialog' | 'alertdialog';
  onSubmit?(e: FormEvent<HTMLFormElement>): void;
}
export const Dialog = wrap<DialogProps>('Dialog');

export interface ConfirmDialogProps {
  open: boolean;
  onClose(): void;
  onConfirm(): unknown;
  title: ReactNode;
  description?: ReactNode;
  confirmLabel: string;
  cancelLabel?: string;
  danger?: boolean;
  /** The user must type this text before the confirm button works. */
  confirmText?: string;
  icon?: string;
  children?: ReactNode;
}
export const ConfirmDialog = wrap<ConfirmDialogProps>('ConfirmDialog');
export const Sheet = wrap<{ open: boolean; onClose(): void; title?: ReactNode; children: ReactNode }>('Sheet');
export const Panel = wrap<Record<string, unknown>>('Panel');

export type MenuItem =
  | { type?: 'item'; id: string; label: string; icon?: string; kbd?: string; danger?: boolean; disabled?: boolean; onSelect(): void }
  | { type: 'separator' }
  | { type: 'heading'; label: string };
export const DropdownMenu = wrap<{
  items: MenuItem[];
  trigger(p: { onClick(e: MouseEvent<HTMLElement>): void; 'aria-haspopup': 'menu'; 'aria-expanded': boolean }): ReactNode;
  'aria-label'?: string;
}>('DropdownMenu');
export const Tooltip = wrap<{ label: ReactNode; children: ReactElement; delay?: number }>('Tooltip');

export interface Series {
  values: number[];
  color?: string;
  label?: string;
}
export const Sparkline = wrap<{ values: number[]; height?: number; color?: string; fill?: boolean; min?: number; max?: number }>('Sparkline');
export const AreaChart = wrap<{ series: Series[]; height?: number; max?: number; min?: number; label?: string }>('AreaChart');

export const toast = {
  ok: (title: string, detail?: string) => getSdk().ui.toast.ok(title, detail),
  err: (title: string, detail?: string) => getSdk().ui.toast.err(title, detail),
  info: (title: string, detail?: string) => getSdk().ui.toast.info(title, detail),
};

/** Hooks of the kit: call inside components. */
export const useIsMobile = (): boolean => getSdk().ui.useIsMobile();
export const useMediaQuery = (q: string): boolean => getSdk().ui.useMediaQuery(q);
