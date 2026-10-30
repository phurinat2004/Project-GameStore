import { useEffect, useState } from 'react'
import AuthForm from './AuthForm.jsx'

function formatPrice(price) {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
  }).format(price)
}

export default function App() {
  const [games, setGames] = useState([])
  const [query, setQuery] = useState('')
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState('loading')
  const [user, setUser] = useState(null)
  const [authLoading, setAuthLoading] = useState(true)
  const [authView, setAuthView] = useState(null)
  const [notice, setNotice] = useState('')

  useEffect(() => {
    const controller = new AbortController()
    const params = new URLSearchParams()
    if (search.trim()) params.set('q', search.trim())

    async function loadGames() {
      setStatus('loading')
      try {
        const response = await fetch(`/api/games?${params}`, {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!response.ok) throw new Error(`Request failed (${response.status})`)
        const data = await response.json()
        if (!Array.isArray(data)) throw new Error('Invalid games response')
        setGames(data)
        setStatus('success')
      } catch (error) {
        if (error.name !== 'AbortError') setStatus('error')
      }
    }

    loadGames()
    return () => controller.abort()
  }, [search])

  useEffect(() => {
    let active = true
    async function loadSession() {
      try {
        const response = await fetch('/api/auth/me', { credentials: 'include' })
        if (response.status === 401) return
        if (!response.ok) throw new Error(`Request failed (${response.status})`)
        const data = await response.json()
        if (active) setUser(data.user)
      } catch {
        if (active) setNotice('Unable to verify your session. Please try again.')
      } finally {
        if (active) setAuthLoading(false)
      }
    }
    loadSession()
    return () => { active = false }
  }, [])

  function submitSearch(event) {
    event.preventDefault()
    setSearch(query)
  }

  async function logout() {
    try {
      const response = await fetch('/api/auth/logout', {
        method: 'POST',
        credentials: 'include',
      })
      if (!response.ok) throw new Error(`Request failed (${response.status})`)
      setUser(null)
      setNotice('You have been logged out.')
    } catch {
      setNotice('Unable to log out. Please try again.')
    }
  }

  return (
    <main className="page-shell">
      <header className="site-header">
        <a className="brand" href="/" aria-label="GameStore home">
          <span className="brand-mark" aria-hidden="true">G</span>
          <span>GAME<span className="brand-accent">STORE</span></span>
        </a>
        <nav className="header-actions" aria-label="Account">
          {authView ? (
            <button className="nav-button" onClick={() => setAuthView(null)} type="button">
              Back to games
            </button>
          ) : authLoading ? (
            <span className="header-note">CHECKING SESSION</span>
          ) : user ? (
            <>
              <span className="member-greeting">Hi, <strong>{user.username}</strong></span>
              <button className="nav-button" onClick={logout} type="button">Log out</button>
            </>
          ) : (
            <>
              <span className="header-note">YOUR NEXT ADVENTURE STARTS HERE</span>
              <button className="nav-button" onClick={() => { setNotice(''); setAuthView('login') }} type="button">
                Log in
              </button>
              <button className="nav-button nav-button-primary" onClick={() => { setNotice(''); setAuthView('register') }} type="button">
                Register
              </button>
            </>
          )}
        </nav>
      </header>

      {notice && <p className="app-notice" role="status">{notice}</p>}
      {authView ? (
        <AuthForm
          key={authView}
          mode={authView}
          onAuthenticated={(authenticatedUser) => {
            setUser(authenticatedUser)
            setAuthView(null)
            setNotice('')
          }}
          onRegistered={() => {
            setAuthView('login')
            setNotice('Account created. You can now log in.')
          }}
          onSwitch={() => setAuthView(authView === 'register' ? 'login' : 'register')}
        />
      ) : (
        <>
          <section className="hero" aria-labelledby="hero-title">
            <div className="hero-copy">
              <p className="eyebrow"><span /> THE PLAYER'S CHOICE</p>
              <h1 id="hero-title">Play something<br /><span> unforgettable.</span></h1>
              <p className="hero-description">
                Find your next favorite. A handpicked collection of worlds worth
                getting lost in.
              </p>
            </div>
            <div className="hero-art" aria-hidden="true">
              <div className="art-ring ring-one" />
              <div className="art-ring ring-two" />
              <div className="art-spark">✳</div>
              <div className="art-label">LEVEL UP<br /><span>YOUR LIBRARY</span></div>
            </div>
          </section>

          <section className="catalog" aria-labelledby="catalog-title">
            <div className="catalog-heading">
              <div>
                <p className="eyebrow">THE COLLECTION</p>
                <h2 id="catalog-title">Explore games<span>.</span></h2>
              </div>
              <form className="search-form" role="search" onSubmit={submitSearch}>
                <label className="sr-only" htmlFor="game-search">Search games</label>
                <input
                  id="game-search"
                  type="search"
                  placeholder="Search games..."
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                />
                <button type="submit" aria-label="Submit search">⌕</button>
              </form>
            </div>

            {status === 'loading' && (
              <p className="catalog-message" role="status">Loading games...</p>
            )}
            {status === 'error' && (
              <p className="catalog-message error-message" role="alert">
                We couldn’t load the games. Check that the GameStore API is running
                and try again.
              </p>
            )}
            {status === 'success' && games.length === 0 && (
              <p className="catalog-message" role="status">No games found. Try another search.</p>
            )}
            {status === 'success' && games.length > 0 && (
              <div className="game-grid">
                {games.map((game) => (
                  <article className="game-card" key={game.id}>
                    <div className="game-art">
                      {game.image_url ? (
                        <img src={game.image_url} alt="" loading="lazy" />
                      ) : (
                        <span className="game-art-placeholder" aria-hidden="true">
                          {game.title.slice(0, 1)}
                        </span>
                      )}
                      <span className="genre-tag">{game.genre}</span>
                    </div>
                    <div className="game-info">
                      <div>
                        <h3>{game.title}</h3>
                        <p>{game.description}</p>
                      </div>
                      <span className="game-price">{formatPrice(game.price)}</span>
                    </div>
                  </article>
                ))}
              </div>
            )}
          </section>
        </>
      )}

      <footer className="site-footer">
        <a className="brand footer-brand" href="/" aria-label="GameStore home">
          <span className="brand-mark" aria-hidden="true">G</span>
          <span>GAME<span className="brand-accent">STORE</span></span>
        </a>
        <span>GOOD GAMES. GREAT ADVENTURES.</span>
      </footer>
    </main>
  )
}
