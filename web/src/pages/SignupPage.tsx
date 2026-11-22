import { useEffect, useState } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { Link, useNavigate } from 'react-router-dom';
import { MessageSquare } from 'lucide-react';
import { echostream } from '../lib/api';
import { ApiError } from '../lib/apiClient';
import { useAuthStore } from '../store/authStore';
import { AuthShell } from '../components/AuthShell';
import { Button, TextField } from '../components/ui';

const schema = z
  .object({
    tenant_name: z.string().optional(),
    display_name: z.string().min(1, 'Display name is required'),
    email: z.string().email('Enter a valid email'),
    password: z.string().min(8, 'Password must be at least 8 characters'),
  });

type FormValues = z.infer<typeof schema>;

export function SignupPage() {
  const navigate = useNavigate();
  const [inviteToken] = useState(() => {
    const url = new URL(window.location.href);
    return new URLSearchParams(url.hash.slice(1)).get('invite') ?? url.searchParams.get('invite') ?? '';
  });
  useEffect(() => {
    if (!inviteToken) return;
    const url = new URL(window.location.href);
    const fragment = new URLSearchParams(url.hash.slice(1));
    fragment.delete('invite');
    url.hash = fragment.toString();
    url.searchParams.delete('invite');
    window.history.replaceState(window.history.state, '', url);
  }, [inviteToken]);
  const hasInvite = inviteToken !== '';

  const setToken = useAuthStore((s) => s.setToken);
  const [formError, setFormError] = useState<string | null>(null);

  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<FormValues>({ resolver: zodResolver(schema) });

  async function onSubmit(values: FormValues) {
    setFormError(null);
    if (!hasInvite && !values.tenant_name?.trim()) {
      setFormError('Enter a workspace name.');
      return;
    }
    try {
      const { token } = await echostream.signup({
        ...values,
        tenant_name: values.tenant_name ?? '',
        invite_token: inviteToken,
      });
      setToken(token);
      navigate('/', { replace: true });
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : 'Signup failed');
    }
  }

  return (
    <AuthShell
      icon={<MessageSquare className="h-6 w-6" />}
      title={hasInvite ? 'Join workspace' : 'Create your workspace'}
      subtitle={
        hasInvite
          ? 'You were invited — fill in your details to join'
          : 'Sign up and start chatting in real time'
      }
    >
      <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
        {!hasInvite && (
          <TextField
            label="Workspace name"
            placeholder="Acme Inc."
            error={errors.tenant_name?.message}
            {...register('tenant_name')}
          />
        )}
        <TextField
          label="Display name"
          placeholder="Ada Lovelace"
          error={errors.display_name?.message}
          {...register('display_name')}
        />
        <TextField
          label="Email"
          type="email"
          autoComplete="email"
          error={errors.email?.message}
          {...register('email')}
        />
        <TextField
          label="Password"
          type="password"
          autoComplete="new-password"
          error={errors.password?.message}
          {...register('password')}
        />
        {formError && (
          <p className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-600 dark:bg-red-950/40">
            {formError}
          </p>
        )}
        <Button type="submit" disabled={isSubmitting}>
          {isSubmitting ? (hasInvite ? 'Joining…' : 'Creating…') : hasInvite ? 'Join workspace' : 'Create workspace'}
        </Button>
      </form>
      <p className="mt-6 text-center text-sm text-slate-500">
        Already have an account?{' '}
        <Link to="/login" className="font-semibold text-indigo-600 hover:underline">
          Log in
        </Link>
      </p>
    </AuthShell>
  );
}
