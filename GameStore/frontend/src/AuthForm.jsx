import { useState } from 'react'

export default function AuthForm({ mode, onAuthenticated, onRegistered, onSwitch }) {
  const isRegister = mode === 'register'
  const [values, setValues] = useState({
    username: '',
    email: '',
    identifier: '',
    password: '',
    confirmPassword: '',
  })
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  function update(event) {
    setValues((current) => ({ ...current, [event.target.name]: event.target.value }))
    setError('')
  }

  async function submit(event) {
    event.preventDefault()
    setError('')

    if (isRegister && !/^[a-zA-Z0-9_]{3,32}$/.test(values.username.trim())) {
      setError('Username must be 3–32 characters and contain only letters, numbers, and underscores.')
      return
    }
    if (values.password.length < 8 || new TextEncoder().encode(values.password).length > 72) {
      setError('Password must be between 8 and 72 bytes.')
      return
    }
    if (isRegister && values.password !== values.confirmPassword) {
      setError('Passwords do not match.')
      return
    }

    setSubmitting(true)
    try {
      const response = await fetch(`/api/auth/${isRegister ? 'register' : 'login'}`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(isRegister
          ? {
              username: values.username,
              email: values.email,
              password: values.password,
              confirmPassword: values.confirmPassword,
            }
          : { identifier: values.identifier, password: values.password }),
      })
      const payload = await response.json()
      if (!response.ok) {
        setError(payload?.error?.message || 'Unable to complete your request. Please try again.')
        return
      }

      if (isRegister) onRegistered()
      else onAuthenticated(payload.user)
    } catch {
      setError('Unable to reach GameStore. Check your connection and try again.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <section className="auth-section" aria-labelledby="auth-title">
      <div className="auth-card">
        <p className="eyebrow">{isRegister ? 'JOIN THE COMMUNITY' : 'WELCOME BACK'}</p>
        <h1 id="auth-title">{isRegister ? 'Create your account.' : 'Good to see you.'}</h1>
        <p className="auth-intro">
          {isRegister
            ? 'Save your place in the GameStore community.'
            : 'Sign in to pick up where your next adventure begins.'}
        </p>

        <form className="auth-form" onSubmit={submit}>
          {isRegister ? (
            <>
              <label htmlFor="register-username">Username</label>
              <input
                autoComplete="username"
                id="register-username"
                maxLength={32}
                minLength={3}
                name="username"
                onChange={update}
                pattern="[A-Za-z0-9_]{3,32}"
                required
                value={values.username}
              />
              <label htmlFor="register-email">Email</label>
              <input
                autoComplete="email"
                id="register-email"
                maxLength={254}
                name="email"
                onChange={update}
                required
                type="email"
                value={values.email}
              />
            </>
          ) : (
            <>
              <label htmlFor="login-identifier">Username or email</label>
              <input
                autoComplete="username"
                id="login-identifier"
                name="identifier"
                onChange={update}
                required
                value={values.identifier}
              />
            </>
          )}

          <label htmlFor={`${mode}-password`}>Password</label>
          <input
            autoComplete={isRegister ? 'new-password' : 'current-password'}
            id={`${mode}-password`}
            maxLength={72}
            minLength={8}
            name="password"
            onChange={update}
            required
            type="password"
            value={values.password}
          />

          {isRegister && (
            <>
              <label htmlFor="register-confirm-password">Confirm password</label>
              <input
                autoComplete="new-password"
                id="register-confirm-password"
                maxLength={72}
                minLength={8}
                name="confirmPassword"
                onChange={update}
                required
                type="password"
                value={values.confirmPassword}
              />
            </>
          )}

          {error && <p className="auth-error" role="alert">{error}</p>}
          <button className="auth-submit" disabled={submitting} type="submit">
            {submitting ? 'Please wait...' : isRegister ? 'Create account' : 'Log in'}
          </button>
        </form>

        <p className="auth-switch">
          {isRegister ? 'Already have an account?' : 'New to GameStore?'}
          {' '}
          <button type="button" onClick={onSwitch}>
            {isRegister ? 'Log in' : 'Create an account'}
          </button>
        </p>
      </div>
    </section>
  )
}
