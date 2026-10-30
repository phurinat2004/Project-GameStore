import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import App from './App.jsx'

const games = [
  {
    id: 1,
    title: 'Starbound',
    description: 'Explore a universe of possibilities.',
    genre: 'Adventure',
    price: 29.99,
    release_date: '2024-01-01',
    image_url: '',
  },
]

const member = {
  id: 1,
  username: 'playerone',
  email: 'playerone@example.com',
  role: 'member',
}

function jsonResponse(data, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => data,
  }
}

function defaultFetch(url) {
  if (url === '/api/auth/me') {
    return Promise.resolve(jsonResponse({ error: { code: 'unauthorized' } }, 401))
  }
  return Promise.resolve(jsonResponse(games))
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('GameStore catalog', () => {
  it('loads games from the API and displays them', async () => {
    vi.stubGlobal('fetch', vi.fn(defaultFetch))

    render(<App />)

    expect(await screen.findByRole('heading', { name: 'Starbound' })).toBeInTheDocument()
    expect(screen.getByText('$29.99')).toBeInTheDocument()
    expect(fetch).toHaveBeenCalledWith('/api/games?', expect.objectContaining({ credentials: 'include' }))
  })

  it('submits the search term to the API', async () => {
    vi.stubGlobal('fetch', vi.fn(defaultFetch))
    const user = userEvent.setup()

    render(<App />)
    await screen.findByRole('heading', { name: 'Starbound' })
    await user.type(screen.getByRole('searchbox', { name: 'Search games' }), 'space')
    await user.click(screen.getByRole('button', { name: 'Submit search' }))

    expect(await screen.findByRole('heading', { name: 'Starbound' })).toBeInTheDocument()
    expect(fetch).toHaveBeenCalledWith('/api/games?q=space', expect.objectContaining({ credentials: 'include' }))
  })

  it('shows an error when the API request fails', async () => {
    vi.stubGlobal('fetch', vi.fn((url) => {
      if (url === '/api/auth/me') return Promise.resolve(jsonResponse({}, 401))
      return Promise.reject(new Error('offline'))
    }))

    render(<App />)

    expect(await screen.findByRole('alert')).toHaveTextContent('couldn’t load the games')
  })

  it('registers a member and then offers the login form', async () => {
    const fetchMock = vi.fn((url) => {
      if (url === '/api/auth/me') return Promise.resolve(jsonResponse({}, 401))
      if (url === '/api/auth/register') return Promise.resolve(jsonResponse({ user: member }, 201))
      return Promise.resolve(jsonResponse(games))
    })
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    render(<App />)
    await user.click(await screen.findByRole('button', { name: 'Register' }))
    await user.type(screen.getByLabelText('Username'), 'playerone')
    await user.type(screen.getByLabelText('Email'), 'playerone@example.com')
    await user.type(screen.getByLabelText('Password'), 'secure-password-123')
    await user.type(screen.getByLabelText('Confirm password'), 'secure-password-123')
    await user.click(screen.getByRole('button', { name: 'Create account' }))

    expect(await screen.findByRole('status')).toHaveTextContent('Account created')
    expect(screen.getByRole('heading', { name: 'Good to see you.' })).toBeInTheDocument()
    const registerCall = fetchMock.mock.calls.find(([url]) => url === '/api/auth/register')
    expect(registerCall[1]).toEqual(expect.objectContaining({
      credentials: 'include',
      method: 'POST',
    }))
    expect(JSON.parse(registerCall[1].body)).toEqual({
      username: 'playerone',
      email: 'playerone@example.com',
      password: 'secure-password-123',
      confirmPassword: 'secure-password-123',
    })
  })

  it('rejects mismatched passwords before sending a registration request', async () => {
    const fetchMock = vi.fn(defaultFetch)
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    render(<App />)
    await user.click(await screen.findByRole('button', { name: 'Register' }))
    await user.type(screen.getByLabelText('Username'), 'playerone')
    await user.type(screen.getByLabelText('Email'), 'playerone@example.com')
    await user.type(screen.getByLabelText('Password'), 'secure-password-123')
    await user.type(screen.getByLabelText('Confirm password'), 'a-different-password')
    await user.click(screen.getByRole('button', { name: 'Create account' }))

    expect(screen.getByRole('alert')).toHaveTextContent('Passwords do not match')
    expect(fetchMock).not.toHaveBeenCalledWith('/api/auth/register', expect.anything())
  })

  it('logs in, shows the member, and logs out without reloading', async () => {
    const fetchMock = vi.fn((url) => {
      if (url === '/api/auth/me') return Promise.resolve(jsonResponse({}, 401))
      if (url === '/api/auth/login') return Promise.resolve(jsonResponse({ user: member }))
      if (url === '/api/auth/logout') return Promise.resolve(jsonResponse(null, 204))
      return Promise.resolve(jsonResponse(games))
    })
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    render(<App />)
    await user.click(await screen.findByRole('button', { name: 'Log in' }))
    await user.type(screen.getByLabelText('Username or email'), 'playerone@example.com')
    await user.type(screen.getByLabelText('Password'), 'secure-password-123')
    await user.click(screen.getByRole('button', { name: 'Log in', exact: true }))

    expect(await screen.findByText('playerone')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Log out' }))

    expect(await screen.findByRole('button', { name: 'Register' })).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('logged out')
    expect(fetchMock).toHaveBeenCalledWith('/api/auth/logout', expect.objectContaining({
      credentials: 'include',
      method: 'POST',
    }))
  })

  it('shows a backend login error without changing guest state', async () => {
    const fetchMock = vi.fn((url) => {
      if (url === '/api/auth/me') return Promise.resolve(jsonResponse({}, 401))
      if (url === '/api/auth/login') {
        return Promise.resolve(jsonResponse({
          error: { code: 'invalid_credentials', message: 'Username/email or password is incorrect.' },
        }, 401))
      }
      return Promise.resolve(jsonResponse(games))
    })
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    render(<App />)
    await user.click(await screen.findByRole('button', { name: 'Log in' }))
    await user.type(screen.getByLabelText('Username or email'), 'playerone')
    await user.type(screen.getByLabelText('Password'), 'wrong-password')
    await user.click(screen.getByRole('button', { name: 'Log in', exact: true }))

    expect(await screen.findByRole('alert')).toHaveTextContent('incorrect')
    expect(screen.getByRole('button', { name: 'Back to games' })).toBeInTheDocument()
  })
})
