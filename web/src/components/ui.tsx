import { clsx } from 'clsx';
import type { ButtonHTMLAttributes, InputHTMLAttributes } from 'react';

export function TextField({
  label,
  error,
  ...props
}: InputHTMLAttributes<HTMLInputElement> & { label: string; error?: string }) {
  return (
    <label className="block">
      <span className="mb-1 block text-sm font-medium text-slate-700 dark:text-slate-200">
        {label}
      </span>
      <input
        className={clsx(
          'w-full rounded-lg border px-3 py-2 text-sm outline-none transition',
          'bg-white text-slate-900 placeholder:text-slate-400',
          'focus:border-indigo-500 focus:ring-2 focus:ring-indigo-500/30',
          'dark:bg-slate-800 dark:text-slate-100 dark:border-slate-700',
          error ? 'border-red-400' : 'border-slate-300',
        )}
        {...props}
      />
      {error && <span className="mt-1 block text-xs text-red-500">{error}</span>}
    </label>
  );
}

export function Button({
  className,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      className={clsx(
        'inline-flex w-full items-center justify-center rounded-lg px-4 py-2 text-sm font-semibold',
        'bg-indigo-600 text-white transition hover:bg-indigo-500',
        'disabled:cursor-not-allowed disabled:opacity-60',
        className,
      )}
      {...props}
    />
  );
}
