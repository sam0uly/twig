defmodule Cache do
  @moduledoc """
  A documented module.
  """

  defstruct entries: %{}, limit: 0x1F

  def lookup(%__MODULE__{entries: entries}, key) do
    case Map.fetch(entries, key) do
      {:ok, value} -> value
      :error -> nil
    end
  end
end
