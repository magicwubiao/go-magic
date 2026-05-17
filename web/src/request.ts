// Simple request wrapper using axios
import axios from 'axios'

const request = axios.create({
  baseURL: '/api',
  timeout: 30000,
})

// Add auth token if available
request.interceptors.request.use((config) => {
  const token = localStorage.getItem('auth_token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

export default request
