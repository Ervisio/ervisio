/** Automatic JSX runtime on top of the SDK's React (see react-shim.ts). */
import { createElement, Fragment } from './react-shim';

type Props = Record<string, unknown> & { children?: unknown };

export { Fragment };

export function jsx(type: unknown, props: Props, key?: string) {
  const { children, ...rest } = props;
  if (key !== undefined) rest.key = key;
  return children === undefined ? createElement(type as string, rest) : createElement(type as string, rest, children as never);
}

export function jsxs(type: unknown, props: Props, key?: string) {
  const { children, ...rest } = props;
  if (key !== undefined) rest.key = key;
  return createElement(type as string, rest, ...(children as never[]));
}

export const jsxDEV = jsx;
