import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { X } from 'lucide-react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { echostream } from '../lib/api';
import type { Channel } from '../lib/types';
import { Button, TextField } from './ui';

const schema = z.object({
  name: z
    .string()
    .min(1, 'Channel name is required')
    .max(80, 'Max 80 characters')
    .regex(/^[a-z0-9_-]+$/, 'Lowercase letters, numbers, hyphens, underscores only'),
  is_private: z.boolean(),
});
type FormValues = z.infer<typeof schema>;

interface Props {
  onClose: () => void;
  onCreated: (channel: Channel) => void;
}

export function CreateChannelModal({ onClose, onCreated }: Props) {
  const queryClient = useQueryClient();
  const [serverError, setServerError] = useState<string | null>(null);

  const { register, handleSubmit, formState: { errors } } = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: { name: '', is_private: false },
  });

  const mutation = useMutation({
    mutationFn: (values: FormValues) =>
      echostream.createChannel(values.name, values.is_private),
    onSuccess: (channel) => {
      queryClient.invalidateQueries({ queryKey: ['channels'] });
      onCreated(channel);
    },
    onError: (err: Error) => setServerError(err.message),
  });

  return (
    // Backdrop
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40"
      onClick={onClose}
    >
      <div
        className="w-full max-w-sm rounded-2xl border border-slate-200 bg-white p-6 shadow-xl dark:border-slate-700 dark:bg-slate-900"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-base font-semibold text-slate-900 dark:text-white">
            Create a channel
          </h2>
          <button
            onClick={onClose}
            className="rounded-md p-1 text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <form
          onSubmit={handleSubmit((v) => mutation.mutate(v))}
          className="space-y-4"
        >
          <TextField
            label="Name"
            placeholder="e.g. general"
            error={errors.name?.message}
            {...register('name')}
          />

          <label className="flex items-center gap-3 text-sm text-slate-700 dark:text-slate-200">
            <input type="checkbox" className="h-4 w-4 rounded" {...register('is_private')} />
            Private channel
          </label>

          {serverError && (
            <p className="text-sm text-red-500">{serverError}</p>
          )}

          <Button type="submit" disabled={mutation.isPending}>
            {mutation.isPending ? 'Creating…' : 'Create channel'}
          </Button>
        </form>
      </div>
    </div>
  );
}
